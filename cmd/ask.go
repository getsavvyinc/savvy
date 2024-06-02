package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	huhSpinner "github.com/charmbracelet/huh/spinner"
	"github.com/getsavvyinc/savvy-cli/client"
	"github.com/getsavvyinc/savvy-cli/cmd/component"
	"github.com/getsavvyinc/savvy-cli/cmd/component/list"
	"github.com/getsavvyinc/savvy-cli/display"
	"github.com/spf13/cobra"
)

var askCmd = &cobra.Command{
	Use:   "ask",
	Short: "Ask Savvy a question and it will generate a command",
	Example: `
  savvy ask # interactive mode
  savvy ask "how do I deploy a k8s daemonset?"
  savvy ask "how do I parse a x509 cert"
  savvy ask "how do I find the process id listening on a port?"
  savvy ask "how do I quit vim?"
  savvy ask "extract filenames from the name key in each line of li_ids.txt" --file /path/to/li_ids.txt
  `,
	Long: `
  Ask Savvy a question and it will generate a command for you.

  If a file path is provided, Savvy will use the contents of the file to generate a command.
  `,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		logger := loggerFromCtx(ctx).With("command", "ask")

		var cl client.Client
		var err error

		cl, err = client.New()
		if err != nil {
			logger.Debug("error creating client", "error", err, "message", "falling back to guest client")
			cl = client.NewGuest()
		}

		// get info about the os from os pkg: mac/darwin, linux, windows
		goos := runtime.GOOS
		if goos == "darwin" {
			goos = "macos, darwin, osx"
		}

		fileData, err := fileData(filePath)
		if err != nil {
			display.Error(err)
			os.Exit(1)
		}

		var question string
		if len(args) > 0 {
			// be defensive: users can pass questions as one string or multiple strings
			question = strings.Join(args[:], " ")
		}

		params := AskParams{
			goos:     goos,
			fileData: fileData,
			filePath: filePath,
		}

		var selectedCommand string
		refine := true
		for refine {
			selectedCommand, refine = runAsk(ctx, cl, question, params)
		}

		if selectedCommand == "" {
			return
		}
		if err := clipboard.WriteAll(selectedCommand); err != nil {
			display.Info(selectedCommand)
			return
		}
		display.Info(fmt.Sprintf("Copied to clipboard: %s", selectedCommand))
	},
}

type AskParams struct {
	goos     string
	fileData []byte
	filePath string
}

func runAsk(ctx context.Context, cl client.Client, question string, askParams AskParams) (string, bool) {
	logger := loggerFromCtx(ctx).With("command", "ask", "method", "runAsk")
	if len(question) == 0 {
		// interactive mode
		text := huh.NewText().Title("Ask Savvy a question").Value(&question)
		form := huh.NewForm(huh.NewGroup(text))
		if err := form.Run(); err != nil {
			display.ErrorWithSupportCTA(err)
			os.Exit(1)
		}
	}

	if len(question) == 0 {
		display.Info("Exiting...")
		return "", false
	}

	qi := client.QuestionInfo{
		Question: question,
		Tags: map[string]string{
			"os": askParams.goos,
		},
		FileData: askParams.fileData,
		FileName: path.Base(askParams.filePath),
	}

	var runbook *client.Runbook
	if err := huhSpinner.New().Title("Savvy is generating an answer for you").Action(func() {
		var err error

		runbook, err = cl.Ask(ctx, qi, nil)
		if err != nil {
			display.FatalErrWithSupportCTA(err)
			return
		}

		if len(runbook.Steps) == 0 {
			err := errors.New("No commands were generated. Please try again")
			display.FatalErrWithSupportCTA(err)
			return
		}
	}).Run(); err != nil {
		logger.Debug("error asking savvy", "error", err.Error())
		display.FatalErrWithSupportCTA(err)
		os.Exit(1)
	}

	rb := component.NewRunbook(&client.GeneratedRunbook{
		Runbook: *runbook,
	})

	m, err := newAskCommandsModel(rb)
	if err != nil {
		display.ErrorWithSupportCTA(err)
		os.Exit(1)
	}

	p := tea.NewProgram(m, tea.WithOutput(programOutput), tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		// TODO: fail gracefully and provide users a link to view the runbook
		display.ErrorWithSupportCTA(fmt.Errorf("could not display runbook: %w", err))
		os.Exit(1)
	}
	if m, ok := result.(*askCommands); ok {
		selectedCommand := m.l.SelectedCommand()
		refinePrompt := m.refinePrompt

		return selectedCommand, refinePrompt
	}
	return "", false
}

type askCommands struct {
	l            list.Model
	refinePrompt bool
}

var RefinePromptHelpBinding = list.NewHelpBinding("p", "refine prompt")

func newAskCommandsModel(runbook *component.Runbook) (*askCommands, error) {
	if runbook == nil {
		return nil, errors.New("runbook is empty")
	}

	listItems := toItems(runbook.Steps)
	l := list.NewModelWithDelegate(listItems, runbook.Title, runbook.URL, list.NewAskDelegate(), RefinePromptHelpBinding)
	return &askCommands{l: l}, nil
}
func (dc *askCommands) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	dc.l.Init()
	return nil
}

func (dc *askCommands) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case list.RefinePromptMsg:
		dc.refinePrompt = true
		return dc, tea.Quit
	}

	m, cmd := dc.l.Update(msg)
	if m, ok := m.(list.Model); ok {
		dc.l = m
	}
	return dc, cmd
}

func (dc *askCommands) View() string {
	return dc.l.View()
}

func fileData(filePath string) ([]byte, error) {
	if filePath == "" {
		return nil, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if stat.Size() > 20*1024 {
		return nil, errors.New("file must be less than 20KB")
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return data, nil
}

var filePath string

func init() {
	rootCmd.AddCommand(askCmd)
	askCmd.Flags().StringVarP(&filePath, "file", "f", "", "File path for ask to read and use while generating an answer")
}
