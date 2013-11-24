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
)

type LevelDbStore struct {
	db *leveldb.DB
}

const jobsKey = "Job:"

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

func (self *LevelDbStore) AllJobs() (jobs []Job, err error) {
	jobs = make([]Job, 0)

	iter := self.db.NewIterator(nil)
	defer iter.Release()

	for iter.Seek([]byte(jobsKey)); strings.HasPrefix(string(iter.Key()), jobsKey); iter.Next() {
		var job Job
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&job)
		if err == nil {
			jobs = append(jobs, job)
		}
	}
	return
}

func (self *LevelDbStore) JobById(id JobId) (job *Job, err error) {
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

func (self *LevelDbStore) JobByName(name string) (job *Job, err error) {
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

	buf, err := ss.Get([]byte(jobKeyById(JobId(jobId))), nil)
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

func (self *LevelDbStore) WriteJob(job Job) (err error) {
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

func (self *LevelDbStore) RunsForJob(job Job) (results []JobRun, err error) {
	results = make([]JobRun, 0)

	iter := self.db.NewIterator(nil)
	prefix := jobRunsKey(job)
	defer iter.Release()

	for iter.Seek([]byte(prefix)); strings.HasPrefix(string(iter.Key()), prefix); iter.Next() {
		var jobRun JobRun
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&jobRun)
		if err == nil {
			results = append(results, jobRun)
		}
	}
	return
}

func (self *LevelDbStore) LastRunForJob(job Job) (result *JobRun, err error) {
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

func (self *LevelDbStore) WriteJobRun(jobRun JobRun) (err error) {
	key := jobRunKey(jobRun)

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&jobRun)
	err = self.db.Put([]byte(key), buf.Bytes(), &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) ProgressForJobRun(jobRun JobRun) (progress *[]JobProgress, err error) {
	ps := make([]JobProgress, 0)

	iter := self.db.NewIterator(nil)
	defer iter.Release()
	prefix := jobProgressPrefix(jobRun)

	for iter.Seek([]byte(prefix)); strings.HasPrefix(string(iter.Key()), prefix); iter.Next() {
		var p JobProgress
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&p)
		if err == nil {
			ps = append(ps, p)
		}
	}
	return &ps, nil
}

func (self *LevelDbStore) WriteJobProgress(jobProgress JobProgress) (err error) {
	key := []byte(jobProgressPrefix(jobProgress.JobRun) + jobProgress.Time.UTC().String())

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&jobProgress)
	err = self.db.Put(key, buf.Bytes(), &levelDbWriteOptions)
	return
}

// ---

func jobKey(job Job) string {
	return jobKeyById(job.Id)
}

func jobKeyById(id JobId) string {
	return fmt.Sprintf("%s:%d", jobsKey, id)
}

func jobNameIndexKey(name string) string {
	return fmt.Sprintf("JobNameToId:%s")
}

func jobRunsKey(job Job) string {
	return fmt.Sprintf("JobRunsForJobId:%d:", job.Id)
}

func jobRunKey(jobRun JobRun) string {
	return jobRunsKey(jobRun.Job) + jobRun.StartedAt.UTC().String()
}

func jobProgressPrefix(jobRun JobRun) string {
	return jobProgressPrefixByJobRunId(jobRun.Id)
}

func jobProgressPrefixByJobRunId(id JobRunId) string {
	return fmt.Sprintf("JobProgressForJobRunId:%d:", id)
}
