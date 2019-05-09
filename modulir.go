package modulir

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brandur/modulir/context"
	"github.com/brandur/modulir/log"
	"github.com/brandur/modulir/parallel"
	"github.com/fsnotify/fsnotify"
	"github.com/rcrowley/goagain"
)

//////////////////////////////////////////////////////////////////////////////
//
//
//
// Public
//
//
//
//////////////////////////////////////////////////////////////////////////////

// Config contains configuration.
type Config struct {
	// Concurrency is the number of concurrent workers to run during the build
	// step.
	//
	// Defaults to 10.
	Concurrency int

	// Log specifies a logger to use.
	//
	// Defaults to an instance of Logger running at informational level.
	Log log.LoggerInterface

	// Port specifies the port on which to serve content from TargetDir over
	// HTTP.
	//
	// Defaults to not running if left unset.
	Port int

	// SourceDir is the directory containing source files.
	//
	// Defaults to ".".
	SourceDir string

	// TargetDir is the directory where the site will be built to.
	//
	// Defaults to "./public".
	TargetDir string
}

// Context contains useful state that can be used by a user-provided build
// function.
type Context = context.Context

// Job is a wrapper for a piece of work that should be executed by the job
// pool.
type Job = parallel.Job

// Build is one of the main entry points to the program. Call this to build
// only one time.
func Build(config *Config, f func(*context.Context) error) {
	// Note: non-blocking so that we can signal into the channel immediately.
	finish := make(chan struct{}, 1)

	// Signal the build loop to finish immediately
	finish <- struct{}{}

	c := initContext(config, nil)
	success := build(c, f, finish)
	if !success {
		os.Exit(1)
	}
}

// BuildLoop is one of the main entry points to the program. Call this to build
// in a perpetual loop.
func BuildLoop(config *Config, f func(*context.Context) error) {
	// Note: blocking to allow us to wait for the build loop to exit
	// gracefully.
	finish := make(chan struct{})

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		config.Log.Errorf("Error starting watcher: %v", err)
		os.Exit(1)
	}
	defer watcher.Close()

	c := initContext(config, watcher)

	// Inherit a `net.Listener` from our parent process or listen anew
	listener, err := getGoagainListener(c)
	if err != nil {
		exitWithError(err)
	}

	go func() {
		if err := serveTargetDirHTTP(c, listener); err != nil {
			exitWithError(err)
		}
	}()

	go build(c, f, finish)

	if _, err := goagain.Wait(listener); err != nil {
		exitWithError(err)
	}

	finish <- struct{}{}
}

//////////////////////////////////////////////////////////////////////////////
//
//
//
// Private
//
//
//
//////////////////////////////////////////////////////////////////////////////

// Runs an infinite built loop until a signal is received over the `finish`
// channel.
//
// Returns true of the last build was successful and false otherwise.
func build(c *context.Context, f func(*context.Context) error, finish chan struct{}) bool {
	rebuild := make(chan struct{})
	rebuildDone := make(chan struct{})

	if c.Watcher != nil {
		go watchChanges(c, c.Watcher, rebuild, rebuildDone)
	}

	for {
		c.Log.Debugf("Start loop")
		c.ResetBuild()

		c.Pool.Run()
		c.Jobs = c.Pool.JobsChan

		err := f(c)

		c.Wait()
		buildDuration := time.Now().Sub(c.Stats.Start)

		errors := c.Pool.Errors
		if err != nil {
			errors = append([]error{err}, errors...)
		}

		if errors != nil {
			for i, err := range errors {
				c.Log.Errorf("Build error: %v", err)

				if i >= 9 {
					c.Log.Errorf("... too many errors (scroll stopping)")
					break
				}
			}
		}

		if !c.FirstRun {
			// We can expect pretty much everything to have ran on the first
			// run, so only print executed jobs on subsequent runs.
			for i, job := range c.Stats.JobsExecuted {
				c.Log.Infof("Executed job: %s (time: %v)", job.Name, job.Duration)

				if i >= 9 {
					c.Log.Infof("... many jobs executed (scroll stopping)")
					break
				}
			}
		}

		c.Log.Infof("Built site in %s (%v / %v job(s) did work; loop took %v)",
			buildDuration, c.Stats.NumJobsExecuted, c.Stats.NumJobs, c.Stats.LoopDuration)

		if c.FirstRun {
			c.FirstRun = false
		} else {
			rebuildDone <- struct{}{}
		}

		select {
		case <-finish:
			c.Log.Infof("Detected finish signal; stopping")
			return len(errors) > 0

		case <-rebuild:
			c.Log.Infof("Detected change; rebuilding")
		}
	}
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(1)
}

func initConfigDefaults(config *Config) *Config {
	if config == nil {
		config = &Config{}
	}

	if config.Concurrency <= 0 {
		config.Concurrency = 10
	}

	if config.Log == nil {
		config.Log = &log.Logger{Level: log.LevelInfo}
	}

	if config.SourceDir == "" {
		config.SourceDir = "."
	}

	if config.TargetDir == "" {
		config.TargetDir = "./public"
	}

	return config
}

func initContext(config *Config, watcher *fsnotify.Watcher) *context.Context {
	config = initConfigDefaults(config)

	pool := parallel.NewPool(config.Log, config.Concurrency)

	c := context.NewContext(&context.Args{
		Log:       config.Log,
		Port:      config.Port,
		Pool:      pool,
		SourceDir: config.SourceDir,
		TargetDir: config.TargetDir,
		Watcher:   watcher,
	})

	return c
}

func shouldRebuild(path string, op fsnotify.Op) bool {
	// A special case, but ignore creates on files that look like Vim backups.
	if strings.HasSuffix(path, "~") && op&fsnotify.Create == fsnotify.Create {
		return false
	}

	if op&fsnotify.Chmod == fsnotify.Chmod {
		return false
	}

	return true
}

func watchChanges(c *context.Context, watcher *fsnotify.Watcher, rebuild, rebuildDone chan struct{}) {
OUTER:
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			c.Log.Debugf("Received event from watcher: %+v", event)

			if !shouldRebuild(event.Name, event.Op) {
				continue
			}

			// Start rebuild
			rebuild <- struct{}{}

			// Wait until rebuild is finished. In the meantime, drain any
			// new events that come in on the watcher's channel.
			for {
				select {
				case <-rebuildDone:
					continue OUTER
				case <-watcher.Events:
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			c.Log.Errorf("Error from watcher:", err)
		}
	}
}
