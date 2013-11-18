package main

import (
	"bytes"
	"encoding/gob"
	"github.com/syndtr/goleveldb/leveldb"
	leveldbopt "github.com/syndtr/goleveldb/leveldb/opt"
	"log"
	"strings"
)

type LevelDbStore struct {
	db *leveldb.DB
}

const jobKeyPrefix = "Job:"

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

	for iter.Seek([]byte(jobKeyPrefix)); strings.HasPrefix(string(iter.Key()), jobKeyPrefix); iter.Next() {
		var job Job
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&job)
		if err == nil {
			jobs = append(jobs, job)
		}
	}
	return
}

func (self *LevelDbStore) JobByName(name string) (job *Job, err error) {
	buf, err := self.db.Get([]byte(jobKeyPrefix+name), nil)
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

func (self *LevelDbStore) WriteJob(job *Job) (err error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&job)

	err = self.db.Put([]byte(jobKeyPrefix+job.Name), buf.Bytes(), &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) ResultsForJob(*Job) (results []JobResult, err error) {
	// TODO
	results = make([]JobResult, 0)
	return
}

func (self *LevelDbStore) WriteJobResult(jobResult JobResult) (err error) {
	// TODO
	return nil
}
