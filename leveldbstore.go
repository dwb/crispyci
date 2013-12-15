package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/syndtr/goleveldb/leveldb"
	leveldbopt "github.com/syndtr/goleveldb/leveldb/opt"
	"log"
	"strconv"
	"strings"

	"github.com/dwb/crispyci/types"
)

type LevelDbStore struct {
	db *leveldb.DB
}

const jobsKey = "Job:"
const jobRunsKey = "JobRun:"

var (
	levelDbWriteOptions = leveldbopt.WriteOptions{Sync: true}
)

func (self *LevelDbStore) Init(filename string) (err error) {
	if self.db != nil {
		return nil
	}
	self.db, err = leveldb.OpenFile(filename, nil)
	return
}

func (self *LevelDbStore) Close() {
	err := self.db.Close()
	if err != nil {
		log.Print(err)
	}
}

func (self *LevelDbStore) AllJobs() (jobs []types.Job, err error) {
	jobs = make([]types.Job, 0)

	iter := self.db.NewIterator(nil)
	defer iter.Release()

	for iter.Seek([]byte(jobsKey)); strings.HasPrefix(string(iter.Key()), jobsKey); iter.Next() {
		var job types.Job
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&job)
		if err == nil {
			jobs = append(jobs, job)
		}
	}
	return
}

func (self *LevelDbStore) JobById(id types.JobId) (job *types.Job, err error) {
	buf, err := self.db.Get([]byte(jobKeyById(id)), nil)
	if err != nil {
		return
	}

	dec := gob.NewDecoder(bytes.NewBuffer(buf))
	err = dec.Decode(&job)
	if err != nil {
		return nil, err
	}
	return
}

func (self *LevelDbStore) JobByName(name string) (job *types.Job, err error) {
	ss, err := self.db.GetSnapshot()
	if err != nil {
		return
	}
	defer ss.Release()

	jobIdBytes, err := ss.Get([]byte(jobNameIndexKey(name)), nil)
	if err != nil {
		return
	}
	jobId, err := strconv.ParseUint(string(jobIdBytes), 10, 64)
	if err != nil || jobId <= 0 {
		return
	}

	buf, err := ss.Get([]byte(jobKeyById(types.JobId(jobId))), nil)
	if err != nil {
		return
	}

	dec := gob.NewDecoder(bytes.NewBuffer(buf))
	err = dec.Decode(&job)
	if err != nil {
		return nil, err
	}
	return
}

func (self *LevelDbStore) WriteJob(job types.Job) (err error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&job)

	batch := new(leveldb.Batch)
	batch.Put([]byte(jobKey(job)), buf.Bytes())
	batch.Put([]byte(jobNameIndexKey(job.Name)),
		[]byte(fmt.Sprintf("%d", job.Id)))

	err = self.db.Write(batch, &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) JobRunById(id types.JobRunId) (jobRun *types.JobRun, err error) {
	buf, err := self.db.Get([]byte(jobRunKeyById(id)), nil)
	if err != nil {
		return
	}

	dec := gob.NewDecoder(bytes.NewBuffer(buf))
	err = dec.Decode(&jobRun)
	if err != nil {
		return nil, err
	}
	return
}

func (self *LevelDbStore) RunsForJob(job types.Job) (results []types.JobRun, err error) {
	results = make([]types.JobRun, 0)

	ss, err := self.db.GetSnapshot()
	if err != nil {
		return
	}
	defer ss.Release()

	iter := ss.NewIterator(nil)
	prefix := jobRunsKeyForJob(job)
	defer iter.Release()

	for iter.Seek([]byte(prefix)); strings.HasPrefix(string(iter.Key()), prefix); iter.Next() {
		var jobRun types.JobRun
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&jobRun)
		if err == nil {
			results = append(results, jobRun)
		}
	}
	return
}

func (self *LevelDbStore) LastRunForJob(job types.Job) (result *types.JobRun, err error) {
	// TODO: we can doubtless do this more efficiently if we don't decode
	// all previous results, but hey it'll be pretty quick as it is.

	results, err := self.RunsForJob(job)
	if err != nil {
		return
	}

	if len(results) > 0 {
		return &results[len(results)-1], nil
	}

	return nil, nil
}

func (self *LevelDbStore) WriteJobRun(jobRun types.JobRun) (err error) {
	batch := new(leveldb.Batch)

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&jobRun)
	toWrite := buf.Bytes()

	batch.Put([]byte(jobRunKey(jobRun)), toWrite)
	batch.Put([]byte(jobRunForJobKey(jobRun)), toWrite)

	err = self.db.Write(batch, &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) DeleteJobRun(jobRun types.JobRun) (err error) {
	batch := new(leveldb.Batch)

	iter := self.db.NewIterator(nil)
	defer iter.Release()
	prefix := jobProgressPrefix(jobRun)

	for iter.Seek([]byte(prefix)); strings.HasPrefix(string(iter.Key()), prefix); iter.Next() {
		batch.Delete(iter.Key())
	}

	batch.Delete([]byte(jobRunForJobKey(jobRun)))
	batch.Delete([]byte(jobRunKey(jobRun)))

	err = self.db.Write(batch, &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) ProgressForJobRun(jobRun types.JobRun) (progress *[]types.JobProgress, err error) {
	ps := make([]types.JobProgress, 0)

	iter := self.db.NewIterator(nil)
	defer iter.Release()
	prefix := jobProgressPrefix(jobRun)

	for iter.Seek([]byte(prefix)); strings.HasPrefix(string(iter.Key()), prefix); iter.Next() {
		var p types.JobProgress
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&p)
		if err == nil {
			ps = append(ps, p)
		}
	}
	return &ps, nil
}

func (self *LevelDbStore) WriteJobProgress(jobProgress types.JobProgress) (err error) {
	key := []byte(jobProgressPrefix(jobProgress.JobRun) + jobProgress.Time.UTC().String())

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&jobProgress)
	err = self.db.Put(key, buf.Bytes(), &levelDbWriteOptions)
	return
}

// ---

func jobKey(job types.Job) string {
	return jobKeyById(job.Id)
}

func jobKeyById(id types.JobId) string {
	return fmt.Sprintf("%s:%s", jobsKey, id)
}

func jobNameIndexKey(name string) string {
	return fmt.Sprintf("JobNameToId:%s")
}

func jobRunsKeyForJob(job types.Job) string {
	return fmt.Sprintf("JobRunsForJobId:%s:", job.Id)
}

func jobRunKey(jobRun types.JobRun) string {
	return jobRunKeyById(jobRun.Id)
}

func jobRunKeyById(id types.JobRunId) string {
	return fmt.Sprintf("%s:%s", jobRunsKey, id)
}

func jobRunForJobKey(jobRun types.JobRun) string {
	return jobRunsKeyForJob(jobRun.Job) + jobRun.StartedAt.UTC().String()
}

func jobProgressPrefix(jobRun types.JobRun) string {
	return jobProgressPrefixByJobRunId(jobRun.Id)
}

func jobProgressPrefixByJobRunId(id types.JobRunId) string {
	return fmt.Sprintf("JobProgressForJobRunId:%s:", id)
}
