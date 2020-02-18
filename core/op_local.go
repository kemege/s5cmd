package core

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/peak/s5cmd/op"
	"github.com/peak/s5cmd/opt"
	"github.com/peak/s5cmd/stats"
	"github.com/termie/go-shutil"
)

func LocalCopy(job *Job, wp *WorkerParams) (stats.StatType, *JobResponse) {
	const opType = stats.FileOp

	var src, dst = job.args[0], job.args[1]

	response := CheckConditions(src, dst, wp, job.opts)
	if response != nil {
		return opType, response
	}

	var err error
	if job.opts.Has(opt.DeleteSource) {
		err = os.Rename(src.arg, dst.arg)
	} else {
		_, err = shutil.Copy(src.arg, dst.arg, true)
	}

	return opType, jobResponse(err)
}

func LocalDelete(job *Job, wp *WorkerParams) (stats.StatType, *JobResponse) {
	const opType = stats.FileOp
	err := os.Remove(job.args[0].arg)
	return opType, jobResponse(err)
}

func BatchLocalCopy(job *Job, wp *WorkerParams) (stats.StatType, *JobResponse) {
	const opType = stats.FileOp

	subCmd := "cp"
	if job.opts.Has(opt.DeleteSource) {
		subCmd = "mv"
	}
	subCmd += job.opts.GetParams()

	st, err := os.Stat(job.args[0].arg)
	walkMode := err == nil && st.IsDir() // walk or glob?

	trimPrefix := job.args[0].arg
	globStart := job.args[0].arg
	if !walkMode {
		loc := strings.IndexAny(trimPrefix, GlobCharacters)
		if loc < 0 {
			return opType, jobResponse(fmt.Errorf("internal error, not a glob: %s", trimPrefix))
		}
		trimPrefix = trimPrefix[:loc]
	} else {
		if !strings.HasSuffix(globStart, string(filepath.Separator)) {
			globStart += string(filepath.Separator)
		}
		globStart = globStart + "*"
	}
	trimPrefix = path.Dir(trimPrefix)
	if trimPrefix == "." {
		trimPrefix = ""
	} else {
		trimPrefix += string(filepath.Separator)
	}

	recurse := job.opts.Has(opt.Recursive)

	err = wildOperationLocal(wp, func(ch chan<- interface{}) error {
		defer func() {
			ch <- nil // send EOF
		}()

		ma, err := filepath.Glob(globStart)
		if err != nil {
			return err
		}
		if len(ma) == 0 {
			if walkMode {
				return nil // Directory empty
			}

			return errors.New("could not find match for glob")
		}

		for _, f := range ma {
			s := f // copy
			st, _ := os.Stat(s)
			if !st.IsDir() {
				ch <- &s
			} else if recurse {
				err = filepath.Walk(s, func(path string, st os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					if st.IsDir() {
						return nil
					}
					ch <- &path
					return nil
				})
				if err != nil {
					return err
				}
			}
		}
		return nil
	}, func(data interface{}) *Job {
		if data == nil {
			return nil
		}
		fn := data.(*string)

		var dstFn string
		if job.opts.Has(opt.Parents) {
			dstFn = *fn
			if strings.Index(dstFn, trimPrefix) == 0 {
				dstFn = dstFn[len(trimPrefix):]
			}
		} else {
			dstFn = filepath.Base(*fn)
		}

		arg1 := NewJobArgument(*fn, nil)
		arg2 := job.args[1].Clone().Append(dstFn, false)

		dir := filepath.Dir(arg2.arg)
		os.MkdirAll(dir, os.ModePerm)

		return job.MakeSubJob(subCmd, op.LocalCopy, []*JobArgument{arg1, arg2}, job.opts)
	})

	return opType, jobResponse(err)
}

func BatchLocalUpload(job *Job, wp *WorkerParams) (stats.StatType, *JobResponse) {
	const opType = stats.FileOp

	subCmd := "cp"
	if job.opts.Has(opt.DeleteSource) {
		subCmd = "mv"
	}
	subCmd += job.opts.GetParams()

	st, err := os.Stat(job.args[0].arg)
	walkMode := err == nil && st.IsDir() // walk or glob?

	trimPrefix := job.args[0].arg
	if !walkMode {
		loc := strings.IndexAny(trimPrefix, GlobCharacters)
		if loc < 0 {
			return opType, jobResponse(fmt.Errorf("internal error, not a glob: %s", trimPrefix))
		}
		trimPrefix = trimPrefix[:loc]
	}
	trimPrefix = path.Dir(trimPrefix)
	if trimPrefix == "." {
		trimPrefix = ""
	} else {
		trimPrefix += string(filepath.Separator)
	}

	err = wildOperationLocal(wp, func(ch chan<- interface{}) error {
		defer func() {
			ch <- nil // send EOF
		}()
		if walkMode {
			err := filepath.Walk(job.args[0].arg, func(path string, st os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if st.IsDir() {
					return nil
				}
				ch <- &path
				return nil
			})
			return err
		} else {
			ma, err := filepath.Glob(job.args[0].arg)
			if err != nil {
				return err
			}
			if len(ma) == 0 {
				return errors.New("could not find match for glob")
			}

			for _, f := range ma {
				s := f // copy
				st, _ = os.Stat(s)
				if !st.IsDir() {
					ch <- &s
				}
			}
			return nil
		}
	}, func(data interface{}) *Job {
		if data == nil {
			return nil
		}
		fn := data.(*string)

		var dstFn string
		if job.opts.Has(opt.Parents) {
			dstFn = *fn
			if strings.Index(dstFn, trimPrefix) == 0 {
				dstFn = dstFn[len(trimPrefix):]
			}
		} else {
			dstFn = filepath.Base(*fn)
		}

		arg1 := NewJobArgument(*fn, nil)
		arg2 := job.args[1].Clone().Append(dstFn, false)
		return job.MakeSubJob(subCmd, op.Upload, []*JobArgument{arg1, arg2}, job.opts)
	})

	return opType, jobResponse(err)
}