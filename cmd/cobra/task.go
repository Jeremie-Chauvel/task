package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-task/task/v3"
	"github.com/go-task/task/v3/args"
	"github.com/go-task/task/v3/internal/logger"
	"github.com/go-task/task/v3/taskfile"
	"github.com/spf13/cobra"
	"mvdan.cc/sh/v3/syntax"
)

// rootCmd represents the base command when called without any subcommands
var (
	version      = ""
	versionFlag  bool
	initTaskFile bool
	list         bool
	listAll      bool
	listJson     bool
	status       bool
	force        bool
	watch        bool
	verbose      bool
	silent       bool
	dry          bool
	summary      bool
	exitCode     bool
	parallel     bool
	concurrency  int
	dir          string
	entrypoint   string
	output       taskfile.Output
	color        bool
	interval     time.Duration
	rootCmd      = &cobra.Command{
		Use:   "task",
		Short: "Task is a task runner / build tool",
		Long: `Task is a task runner / build tool that aims to be simpler and easier to use than, for example, GNU Make.

	Runs the specified task(s). Falls back to the "default" task if no task name
	was specified, or lists all tasks if an unknown task name was specified.

	Example: 'task hello' with the following 'Taskfile.yml' file will generate an
	'output.txt' file with the content "hello".

	'''
	version: '3'
	tasks:
		hello:
			cmds:
				- echo "I am going to write a file named 'output.txt' now."
				- echo "hello" > output.txt
			generates:
				- output.txt
	'''`,
		Run: run,
	}
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {

	rootCmd.PersistentFlags().BoolVar(&versionFlag, "version", false, "show Task version")
	rootCmd.PersistentFlags().BoolVarP(&initTaskFile, "init", "i", false, "creates a new Taskfile.yaml in the current folder")
	rootCmd.PersistentFlags().BoolVarP(&list, "list", "l", false, "lists tasks with description of current Taskfile")
	rootCmd.PersistentFlags().BoolVarP(&listAll, "list-all", "a", false, "lists tasks with or without a description")
	rootCmd.PersistentFlags().BoolVarP(&listJson, "json", "j", false, "formats task list as json")
	rootCmd.PersistentFlags().BoolVar(&status, "status", false, "exits with non-zero exit code if any of the given tasks is not up-to-date")
	rootCmd.PersistentFlags().BoolVarP(&force, "force", "f", false, "forces execution even when the task is up-to-date")
	rootCmd.PersistentFlags().BoolVarP(&watch, "watch", "w", false, "enables watch of the given task")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enables verbose mode")
	rootCmd.PersistentFlags().BoolVarP(&silent, "silent", "s", false, "disables echoing")
	rootCmd.PersistentFlags().BoolVarP(&parallel, "parallel", "p", false, "executes tasks provided on command line in parallel")
	rootCmd.PersistentFlags().BoolVarP(&dry, "dry", "n", false, "compiles and prints tasks in the order that they would be run, without executing them")
	rootCmd.PersistentFlags().BoolVar(&summary, "summary", false, "show summary about a task")
	rootCmd.PersistentFlags().BoolVarP(&exitCode, "exit-code", "x", false, "pass-through the exit code of the task command")
	rootCmd.PersistentFlags().StringVarP(&dir, "dir", "d", "", "sets directory of execution")
	rootCmd.PersistentFlags().StringVarP(&entrypoint, "taskfile", "t", "", `choose which Taskfile to run. Defaults to "Taskfile.yml"`)
	rootCmd.PersistentFlags().StringVarP(&output.Name, "output", "o", "", "sets output style: [interleaved|group|prefixed]")
	rootCmd.PersistentFlags().StringVar(&output.Group.Begin, "output-group-begin", "", "message template to print before a task's grouped output")
	rootCmd.PersistentFlags().StringVar(&output.Group.End, "output-group-end", "", "message template to print after a task's grouped output")
	rootCmd.PersistentFlags().BoolVarP(&color, "color", "c", true, "colored output. Enabled by default. Set flag to false or use NO_COLOR=1 to disable")
	rootCmd.PersistentFlags().IntVarP(&concurrency, "concurrency", "C", 0, "limit number tasks to run concurrently")
	rootCmd.PersistentFlags().DurationVarP(&interval, "interval", "I", 0, "interval to watch for changes")
	// mark flags as mutually exclusive
	rootCmd.MarkFlagsMutuallyExclusive("dir", "taskfile")

}

func main() {
	Execute()
}

func run(cmd *cobra.Command, arguments []string) {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	if versionFlag {
		fmt.Printf("Task version: %s\n", getVersion())
		return
	}

	if initTaskFile {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		if err := task.InitTaskfile(os.Stdout, wd); err != nil {
			log.Fatal(err)
		}
		return
	}

	if entrypoint != "" {
		dir = filepath.Dir(entrypoint)
		entrypoint = filepath.Base(entrypoint)
	}

	if output.Name != "group" {
		if output.Group.Begin != "" {
			log.Fatal("task: You can't set --output-group-begin without --output=group")
			return
		}
		if output.Group.End != "" {
			log.Fatal("task: You can't set --output-group-end without --output=group")
			return
		}
	}

	executor := task.Executor{
		Force:       force,
		Watch:       watch,
		Verbose:     verbose,
		Silent:      silent,
		Dir:         dir,
		Dry:         dry,
		Entrypoint:  entrypoint,
		Summary:     summary,
		Parallel:    parallel,
		Color:       color,
		Concurrency: concurrency,
		Interval:    interval,

		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,

		OutputStyle: output,
	}

	var listOptions = task.NewListOptions(list, listAll, listJson)
	if err := listOptions.Validate(); err != nil {
		log.Fatal(err)
	}

	if (listOptions.ShouldListTasks()) && silent {
		executor.ListTaskNames(listAll)
		return
	}

	if err := executor.Setup(); err != nil {
		log.Fatal(err)
	}
	v, err := executor.Taskfile.ParsedVersion()
	if err != nil {
		log.Fatal(err)
		return
	}

	if listOptions.ShouldListTasks() {
		if foundTasks, err := executor.ListTasks(listOptions); !foundTasks || err != nil {
			os.Exit(1)
		}
		return
	}

	var (
		calls   []taskfile.Call
		globals *taskfile.Vars
	)

	tasksAndVars, cliArgs, err := getArgs(cmd, arguments)
	if err != nil {
		log.Fatal(err)
	}

	if v >= 3.0 {
		calls, globals = args.ParseV3(tasksAndVars...)
	} else {
		calls, globals = args.ParseV2(tasksAndVars...)
	}

	globals.Set("CLI_ARGS", taskfile.Var{Static: cliArgs})
	executor.Taskfile.Vars.Merge(globals)

	if !watch {
		executor.InterceptInterruptSignals()
	}

	ctx := context.Background()

	if status {
		if err := executor.Status(ctx, calls...); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := executor.Run(ctx, calls...); err != nil {
		executor.Logger.Errf(logger.Red, "%v", err)

		if exitCode {
			if err, ok := err.(*task.TaskRunError); ok {
				os.Exit(err.ExitCode())
			}
		}
		os.Exit(1)
	}
}

func getArgs(cmd *cobra.Command, arguments []string) ([]string, string, error) {
	var doubleDashPos = cmd.ArgsLenAtDash()

	if doubleDashPos == -1 {
		return arguments, "", nil
	}

	var quotedCliArgs []string
	for _, arg := range arguments[doubleDashPos:] {
		quotedCliArg, err := syntax.Quote(arg, syntax.LangBash)
		if err != nil {
			return nil, "", err
		}
		quotedCliArgs = append(quotedCliArgs, quotedCliArg)
	}
	return arguments[:doubleDashPos], strings.Join(quotedCliArgs, " "), nil
}

func getVersion() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}

	version = info.Main.Version
	if info.Main.Sum != "" {
		version += fmt.Sprintf(" (%s)", info.Main.Sum)
	}

	return version
}
