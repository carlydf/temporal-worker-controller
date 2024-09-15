package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/fatih/color"
)

var (
	faintColor   = color.New(color.Faint)
	commandColor = color.New(color.FgHiBlue)
)

var (
	skaffoldRunCmd      = newCommand(`skaffold run --profile demo`)
	gitResetWorkflowCmd = newCommand(`git checkout internal/demo/worker/workflow.go`)
)

type demoCommand struct {
	command string
	// If set, the command will automatically be killed after this duration.
	watchDuration time.Duration
}

func newCommand(command string) demoCommand {
	return demoCommand{command: command, watchDuration: 0}
}

func (c demoCommand) WithWatchDuration(d time.Duration) demoCommand {
	return demoCommand{
		command:       c.command,
		watchDuration: d,
	}
}

type demoStep struct {
	description string
	commands    []demoCommand
}

func (ds demoStep) RunAfterConfirmation(ctx context.Context) error {
	// Print the command before running it
	if len(ds.commands) > 0 {
		printConsoleCommand(ds.commands[0].command)
		// wait for ENTER key
		if _, err := fmt.Scanln(); err != nil {
			return fmt.Errorf("error reading input: %w", err)
		}
	}

	return ds.run(ctx, false)
}

func (ds demoStep) run(ctx context.Context, printFirstCommand bool) error {
	//_, _ = faintColor.Printf("# %s\n", ds.description)
	for i, c := range ds.commands {
		if i != 0 || printFirstCommand {
			// Print the command before running it
			printConsoleCommand(c.command)
		}
		// Run the command
		if err := func() error {
			var (
				commandCtx = ctx
				isWatch    = c.watchDuration > 0
			)
			if isWatch {
				c, cancel := context.WithTimeout(ctx, c.watchDuration)
				defer cancel()
				commandCtx = c
			}

			cmd := exec.CommandContext(commandCtx, "sh", "-c", c.command)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			err := cmd.Run()
			if isWatch {
				return ignoreExecKillError(err)
			}
			return err
		}(); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	steps := []demoStep{
		{
			"Ensure worker is up to date",
			[]demoCommand{
				newCommand(`skaffold run --profile demo`),
				newCommand(`kubectl get deployments,pods`),
			},
		},
		{
			"Switch to workflow.Sleep using a patch/version check",
			[]demoCommand{newCommand(`git apply ./internal/demo/changes/version-gate.patch`)},
		},
		{
			"Remove the patch/version check",
			[]demoCommand{
				gitResetWorkflowCmd,
				newCommand(`git apply ./internal/demo/changes/no-version-gate.patch`),
			},
		},
		{
			"Deploy the change to workflow.Sleep",
			[]demoCommand{
				newCommand(`git add internal/demo/worker/workflow.go`),
				newCommand(`git commit -m "Use workflow.Sleep instead of time.Sleep (no version gate)"`),
				//newCommand(`git push`),
				skaffoldRunCmd,
				newCommand(`kubectl get deployments -w`).WithWatchDuration(5 * time.Second),
				newCommand(`kubectl get pods`),
			},
		},
		{
			"Inspect worker status: the deprecated version should still be reachable.",
			[]demoCommand{
				newCommand(`kubectl get -o yaml temporalworker sample | yq '.status' | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken' | yq`),
			},
		},
		{
			"Revert the changes",
			[]demoCommand{
				newCommand(`git reset HEAD~1`),
				gitResetWorkflowCmd,
				skaffoldRunCmd,
			},
		},
	}

	runDemo(steps)
}

func runDemo(steps []demoStep) {
	for _, s := range steps {
		// Print the description
		//fmt.Printf("$ %s", faintColor.Sprintf("# %s [ENTER] ", s.description))
		printConsoleComment(s.description + " [ENTER] ")
		// Print the command before running it
		printConsoleCommand(c.command)
		// wait for ENTER key
		if _, err := fmt.Scanln(); err != nil {
			log.Fatalf("Error reading input: %v", err)
		}

		// Clear the console
		//if err := clearConsole(); err != nil {
		//	log.Fatalf("Error clearing console: %v", err)
		//}

		// Run the command
		if err := s.RunAfterConfirmation(context.Background()); err != nil {
			log.Fatalf("Error running command: %v", err)
		}
	}
	_, _ = faintColor.Println("# Demo complete!")
}

func clearConsole() error {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func printConsoleComment(comment string) {
	fmt.Printf("$ %s", faintColor.Sprint("# "+comment))
}

func printConsoleCommand(command string) {
	fmt.Printf("$ %s\n", commandColor.Sprint(command))
}

func ignoreExecKillError(err error) error {
	// Extract the exit code
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == -1 {
			return nil
		}
		//return nil
	}
	return err
}
