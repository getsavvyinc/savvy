package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/creack/pty"
	"github.com/getsavvyinc/savvy-cli/client"
	"github.com/getsavvyinc/savvy-cli/cmd/component"
	"github.com/getsavvyinc/savvy-cli/display"
	"github.com/getsavvyinc/savvy-cli/server"
	"github.com/getsavvyinc/savvy-cli/shell"
	"github.com/spf13/cobra"
)

// historyCmd represents the history command
var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Create a runbook from your shell history",
	Long: `Create a runbook from  a selection of the last 100 commands in your shell history.
  Savvy can expand all aliases used in your shell history without running the commands.`,
	Hidden: true,
	Run:    recordHistory,
}

func init() {
	recordCmd.AddCommand(historyCmd)
}

func recordHistory(cmd *cobra.Command, _ []string) {
	ctx := cmd.Context()
	logger := loggerFromCtx(ctx)
	cl, err := client.New()
	if err != nil && errors.Is(err, client.ErrInvalidClient) {
		display.Error(errors.New("You must be logged in to record a runbook. Please run `savvy login`"))
		os.Exit(1)
	}

	sh := shell.New("/tmp/savvy-socket")
	lines, err := sh.TailHistory(ctx)
	if err != nil {
		display.FatalErrWithSupportCTA(err)
	}

	selectedHistory := allowUserToSelectCommands(lines)
	commands, err := expandHistory(logger, sh, selectedHistory)
	if err != nil {
		display.FatalErrWithSupportCTA(err)
	}
	if len(commands) == 0 {
		display.Error(errors.New("No commands were recorded"))
		return
	}

	gctx, cancel := context.WithCancel(ctx)
	gm := component.NewGenerateRunbookModel(commands, cl)
	p := tea.NewProgram(gm, tea.WithOutput(programOutput), tea.WithContext(gctx))
	if _, err := p.Run(); err != nil {
		err = fmt.Errorf("failed to generate runbook: %w", err)
		display.ErrorWithSupportCTA(err)
		os.Exit(1)
	}

	// ensure the bubble tea program is finished before we start the next one
	logger.Debug("wait for bubbletea program", "component", "generate runbook", "status", "running")
	cancel()
	p.Wait()
	logger.Debug("wait for bubbletea program", "component", "generate runbook", "status", "finished")

	runbook := <-gm.RunbookCh()
	m, err := newDisplayCommandsModel(runbook)
	if err != nil {
		display.FatalErrWithSupportCTA(err)
	}

	p = tea.NewProgram(m, tea.WithOutput(programOutput), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		logger.Debug("failed to display runbook", "error", err.Error())
		display.Info("View and edit your runbook online at: " + runbook.URL)
		os.Exit(1)
	}
	if runbook.URL != "" {
		display.Success("View and edit your runbook online at: " + runbook.URL)
	}
}

func allowUserToSelectCommands(history []string) (selectedHistory []string) {
	var options []huh.Option[string]
	for i, cmd := range history {
		options = append(options, huh.NewOption(fmt.Sprintf("%d %s", i+1, cmd), cmd))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Savvy History").
				Description("Press x to include/exclude commands in your Runbook").
				Value(&selectedHistory).
				Height(33).
				Options(options...),
		),
	)

	if err := form.Run(); err != nil {
		log.Fatal(err)
	}
	return
}

func expandHistory(logger *slog.Logger, sh shell.Shell, rawCommands []string) ([]string, error) {
	logger.Debug("expanding history", "commands", rawCommands)
	ss, err := server.NewUnixSocketServerWithDefaultPath()
	if err != nil {
		return nil, err
	}
	go ss.ListenAndServe()
	defer func() {
		ss.Close()
	}()

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer func() {
		cancelCtx()
	}()

	c, err := sh.SpawnHistoryExpander(ctx)
	if err != nil {
		err := fmt.Errorf("failed to start history recording: %w", err)
		return nil, err
	}

	ptmx, err := pty.Start(c)
	if err != nil {
		return nil, err
	}
	// Make sure to close the pty at the end.
	defer ptmx.Close()

	// io.Copy blocks till ptmx is closed.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(io.Discard, ptmx)
	}()

	for _, cmd := range rawCommands {
		if _, err := fmt.Fprintln(ptmx, cmd); err != nil {
			return nil, err
		}
	}
	ptmx.Write([]byte{4}) // EOT

	for len(ss.Commands()) < len(rawCommands) {
		// wait for all commands to be processed
		logger.Debug("waiting for all commands to be processed", "processed", len(ss.Commands()), "total", len(rawCommands))
		time.Sleep(1 * time.Second)
	}

	logger.Debug("waiting for wg.Wait()")
	wg.Wait()
	logger.Debug("wg.Wait() finished")
	logger.Debug("waitng for c.Wait()")
	cancelCtx()
	c.Wait()
	logger.Debug("c.Wait() finished")
	return ss.Commands(), nil
}
