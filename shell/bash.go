package shell

import (
	"context"
	"os"
	"os/exec"
	"text/template"
	"time"
)

type bash struct {
	shellCmd string
	// Exported to use in template
	SocketPath string
}

var _ Shell = (*bash)(nil)

// Adapted from: https://github.com/sbstp/kubie/
const bashrcScript = `
RED=$(tput setaf 1)
RESET=$(tput sgr0)

SAVVY_LOGIN_SHELL=0
case "$OSTYPE" in
  solaris*) SAVVY_LOGIN_SHELL=1;;
  darwin*)  SAVVY_LOGIN_SHELL=1;;
  linux*)   SAVVY_LOGIN_SHELL=1;;
  bsd*)     SAVVY_LOGIN_SHELL=1;;
  msys*)    echo "windows os is not supported" ;;
  cygwin*)  echo "windows os is not supported" ;;
  *)        echo "unknown: $OSTYPE" ;;
esac

SAVVY_INPUT_FILE={{.SocketPath}}

# Reference for loading behavior
# https://shreevatsa.wordpress.com/2008/03/30/zshbash-startup-files-loading-order-bashrc-zshrc-etc/


if [[ "$SAVVY_LOGIN_SHELL" == "1" ]] ; then
    if [[ -f "/etc/profile" ]] ; then
        source "/etc/profile"
    fi

    if [[ -f "$HOME/.bash_profile" ]] ; then
        source "$HOME/.bash_profile"
    elif [[ -f "$HOME/.bash_login" ]] ; then
        source "$HOME/.bash_login"
    elif [[ -f "$HOME/.profile" ]] ; then
        source "$HOME/.profile"
    fi
else
    if [[ -f "/etc/bash.bashrc" ]] ; then
        source "/etc/bash.bashrc"
    fi

    if [[ -f "$HOME/.bashrc" ]] ; then
        source "$HOME/.bashrc"
    fi
fi

if ! type savvy_cmd_pre_exec >/dev/null 2>&1; then
echo "${RED} Your shell is not configured to use Savvy. Please run the following commands: ${RESET}"
echo
echo "${RED}> echo 'eval \"\$(savvy init bash)\"' >> ~/.bashrc${RESET}"
echo "${RED}> source ~/.bashrc${RESET}"
exit 1
fi

echo
echo "Type 'exit' or press 'ctrl+d' to stop recording."
`

var bashTemplate *template.Template

func init() {
	bashTemplate = template.Must(template.New("bash").Parse(bashrcScript))
}

func (b *bash) Spawn(ctx context.Context) (*exec.Cmd, error) {
	// Create a temporary file to store the script
	tmpDir := os.TempDir()
	bashrc, err := os.CreateTemp(tmpDir, "savvy-bashrc-*.bash")
	if err != nil {
		return nil, err
	}
	defer bashrc.Close()

	if err := bashTemplate.Execute(bashrc, b); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, b.shellCmd, "--rcfile", bashrc.Name())
	cmd.Env = append(os.Environ(), "SAVVY_CONTEXT=1")
	cmd.WaitDelay = 500 * time.Millisecond
	return cmd, nil
}

func (b *bash) TailHistory(ctx context.Context) ([]string, error) {
	return nil, nil
}
