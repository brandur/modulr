package context

import (
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brandur/modulr/log"
	"github.com/brandur/modulr/parallel"
)

// Args are the set of arguments accepted by NewContext.
type Args struct {
	Concurrency int
	Log         log.LoggerInterface
	Pool        *parallel.Pool
	SourceDir   string
	TargetDir   string
}

// Context contains useful state that can be used by a user-provided build
// function.
type Context struct {
	// Concurrency is the number of concurrent workers to run during the build
	// step.
	Concurrency int

	// Jobs is a channel over which jobs to be done are transmitted.
	Jobs chan func() error

	// Log is a logger that can be used to print information.
	Log log.LoggerInterface

	// SourceDir is the directory containing source files.
	SourceDir string

	// Stats tracks various statistics about the build process.
	//
	// Statistics are reset between build loops, but are cumulative between
	// build phases within a loop (i.e. calls to Wait).
	Stats *Stats

	// TargetDir is the directory where the site will be built to.
	TargetDir string

	// fileModTimeCache remembers the last modified times of files.
	fileModTimeCache *FileModTimeCache

	// forced indicates whether change checking should be bypassed.
	forced bool

	// pool is the job pool used to build the static site.
	pool *parallel.Pool
}

// NewContext initializes and returns a new Context.
func NewContext(args *Args) *Context {
	return &Context{
		Concurrency: args.Concurrency,
		Jobs:        args.Pool.JobsChan,
		Log:         args.Log,
		SourceDir:   args.SourceDir,
		Stats:       &Stats{},
		TargetDir:   args.TargetDir,

		fileModTimeCache: NewFileModTimeCache(args.Log),
		pool:             args.Pool,
	}
}

// IsUnchanged returns whether the target path's modified time has changed since
// the last time it was checked. It also saves the last modified time for
// future checks.
//
// TODO: It also makes sure the root path is being watched.
func (c *Context) IsUnchanged(path string) bool {
	unchanged := c.fileModTimeCache.isUnchanged(path)

	if !unchanged || c.Forced() {
		atomic.AddInt64(&c.Stats.NumJobsExecuted, 1)
	}

	return unchanged
}

// Forced returns whether change checking is disabled in the current context.
//
// Functions using a forced context still return the right value for their
// unchanged return, but execute all their work.
//
// TODO: Rename to IsForced to match IsUnchanged.
func (c *Context) Forced() bool {
	return c.forced
}

// ForcedContext returns a copy of the current Context for which change
// checking is disabled.
//
// Functions using a forced context still return the right value for their
// unchanged return, but execute all their work.
func (c *Context) ForcedContext() *Context {
	forceC := c.clone()
	forceC.forced = true
	return forceC
}

// Wait waits on the job pool to execute its current round of jobs.
//
// Returns true if the round of jobs all executed successfully, and false
// otherwise. In the latter case, a work function should return so that the
// modulr main loop can print the errors that occurred.
//
// If all jobs were successful, the worker pool is restarted so that more jobs
// can be queued. If it wasn't, the jobs channel will be closed, and trying to
// enqueue a new one will panic.
func (c *Context) Wait() bool {
	// Wait for work to finish.
	c.pool.Wait()

	c.Stats.NumJobs += int64(c.pool.NumJobs)

	if c.pool.Errors != nil {
		return false
	}

	// Then start the pool again, which also has the side effect of
	// reinitializing anything that needs to be reinitialized.
	c.pool.Run()

	return true
}

// clone clones the current Context.
func (c *Context) clone() *Context {
	return &Context{
		Concurrency: c.Concurrency,
		Log:         c.Log,
		SourceDir:   c.SourceDir,
		Stats:       c.Stats,
		TargetDir:   c.TargetDir,

		fileModTimeCache: c.fileModTimeCache,
		forced:           c.forced,
	}
}

// FileModTimeCache tracks the last modified time of files seen so a
// determination can be made as to whether they need to be recompiled.
type FileModTimeCache struct {
	log              log.LoggerInterface
	mu               sync.Mutex
	pathToModTimeMap map[string]time.Time
}

// NewFileModTimeCache returns a new FileModTimeCache.
func NewFileModTimeCache(log log.LoggerInterface) *FileModTimeCache {
	return &FileModTimeCache{
		log:              log,
		pathToModTimeMap: make(map[string]time.Time),
	}
}

// isUnchanged returns whether the target path's modified time has changed since
// the last time it was checked. It also saves the last modified time for
// future checks.
func (c *FileModTimeCache) isUnchanged(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			c.log.Errorf("Error stat'ing file: %v", err)
		}
		return false
	}

	modTime := stat.ModTime()

	c.mu.Lock()
	lastModTime, ok := c.pathToModTimeMap[path]
	c.pathToModTimeMap[path] = modTime
	c.mu.Unlock()

	if !ok {
		return false
	}

	changed := lastModTime.Before(modTime)
	if !changed {
		c.log.Debugf("context: No changes to source: %s", path)
	}

	return !changed
}

// Stats tracks various statistics about the build process.
type Stats struct {
	// NumJobs is the total number of jobs generated for the build loop.
	NumJobs int64

	// NumJobsExecuted is the number of jobs that did some kind of heavier
	// lifting during the build loop. i.e. Those that either (1) detected a
	// changed source and rand normally, or (2) were forced to run with a
	// forced context.
	NumJobsExecuted int64

	// Start is the start time of the build loop.
	Start time.Time
}

// Reset resets statistics.
func (s *Stats) Reset() {
	s.NumJobs = 0
	s.NumJobsExecuted = 0
	s.Start = time.Now()
}
