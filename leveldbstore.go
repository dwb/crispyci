package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/syndtr/goleveldb/leveldb"
	leveldbopt "github.com/syndtr/goleveldb/leveldb/opt"
	leveldbutil "github.com/syndtr/goleveldb/leveldb/util"
	"log"
	"strconv"
	"strings"

	"github.com/dwb/crispyci/types"
)

type LevelDbStore struct {
	db *leveldb.DB
}

const projectsKey = "Project:"
const projectRunsKey = "ProjectRun:"

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

func (self *LevelDbStore) AllProjects() (projects []types.Project, err error) {
	projects = make([]types.Project, 0)

	iter := self.db.NewIterator(&leveldbutil.Range{Start: []byte(projectsKey)},
								nil)
	defer iter.Release()

	for ; strings.HasPrefix(string(iter.Key()), projectsKey); iter.Next() {
		var project types.Project
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&project)
		if err == nil {
			projects = append(projects, project)
		}
	}
	return
}

func (self *LevelDbStore) ProjectById(id types.ProjectId) (project *types.Project, err error) {
	buf, err := self.db.Get([]byte(projectKeyById(id)), nil)
	if err != nil {
		return
	}

	dec := gob.NewDecoder(bytes.NewBuffer(buf))
	err = dec.Decode(&project)
	if err != nil {
		return nil, err
	}
	return
}

func (self *LevelDbStore) ProjectByName(name string) (project *types.Project, err error) {
	ss, err := self.db.GetSnapshot()
	if err != nil {
		return
	}
	defer ss.Release()

	projectIdBytes, err := ss.Get([]byte(projectNameIndexKey(name)), nil)
	if err != nil {
		return
	}
	projectId, err := strconv.ParseUint(string(projectIdBytes), 10, 64)
	if err != nil || projectId <= 0 {
		return
	}

	buf, err := ss.Get([]byte(projectKeyById(types.ProjectId(projectId))), nil)
	if err != nil {
		return
	}

	dec := gob.NewDecoder(bytes.NewBuffer(buf))
	err = dec.Decode(&project)
	if err != nil {
		return nil, err
	}
	return
}

func (self *LevelDbStore) WriteProject(project types.Project) (err error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&project)

	batch := new(leveldb.Batch)
	batch.Put([]byte(projectKey(project)), buf.Bytes())
	batch.Put([]byte(projectNameIndexKey(project.Name)),
		[]byte(fmt.Sprintf("%d", project.Id)))

	err = self.db.Write(batch, &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) ProjectRunById(id types.ProjectRunId) (projectRun *types.ProjectRun, err error) {
	buf, err := self.db.Get([]byte(projectRunKeyById(id)), nil)
	if err != nil {
		return
	}

	dec := gob.NewDecoder(bytes.NewBuffer(buf))
	err = dec.Decode(&projectRun)
	if err != nil {
		return nil, err
	}
	return
}

func (self *LevelDbStore) RunsForProject(project types.Project) (results []types.ProjectRun, err error) {
	results = make([]types.ProjectRun, 0)

	ss, err := self.db.GetSnapshot()
	if err != nil {
		return
	}
	defer ss.Release()

	prefix := projectRunsKeyForProject(project)
	iter := ss.NewIterator(&leveldbutil.Range{Start: []byte(prefix)}, nil)
	defer iter.Release()

	for ; strings.HasPrefix(string(iter.Key()), prefix); iter.Next() {
		var projectRun types.ProjectRun
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&projectRun)
		if err == nil {
			results = append(results, projectRun)
		}
	}
	return
}

func (self *LevelDbStore) LastRunForProject(project types.Project) (result *types.ProjectRun, err error) {
	// TODO: we can doubtless do this more efficiently if we don't decode
	// all previous results, but hey it'll be pretty quick as it is.

	results, err := self.RunsForProject(project)
	if err != nil {
		return
	}

	if len(results) > 0 {
		return &results[len(results)-1], nil
	}

	return nil, nil
}

func (self *LevelDbStore) WriteProjectRun(projectRun types.ProjectRun) (err error) {
	batch := new(leveldb.Batch)

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&projectRun)
	toWrite := buf.Bytes()

	batch.Put([]byte(projectRunKey(projectRun)), toWrite)
	batch.Put([]byte(projectRunForProjectKey(projectRun)), toWrite)

	err = self.db.Write(batch, &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) DeleteProjectRun(projectRun types.ProjectRun) (err error) {
	batch := new(leveldb.Batch)

	prefix := projectProgressPrefix(projectRun)
	iter := self.db.NewIterator(&leveldbutil.Range{Start: []byte(prefix)}, nil)
	defer iter.Release()

	for ; strings.HasPrefix(string(iter.Key()), prefix); iter.Next() {
		batch.Delete(iter.Key())
	}

	batch.Delete([]byte(projectRunForProjectKey(projectRun)))
	batch.Delete([]byte(projectRunKey(projectRun)))

	err = self.db.Write(batch, &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) ProgressForProjectRun(projectRun types.ProjectRun) (progress *[]types.ProjectProgress, err error) {
	ps := make([]types.ProjectProgress, 0)

	prefix := projectProgressPrefix(projectRun)
	iter := self.db.NewIterator(&leveldbutil.Range{Start: []byte(prefix)}, nil)
	defer iter.Release()

	for ; strings.HasPrefix(string(iter.Key()), prefix); iter.Next() {
		var p types.ProjectProgress
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&p)
		if err == nil {
			ps = append(ps, p)
		}
	}
	return &ps, nil
}

func (self *LevelDbStore) WriteProjectProgress(projectProgress types.ProjectProgress) (err error) {
	key := []byte(projectProgressPrefix(projectProgress.ProjectRun) + projectProgress.Time.UTC().String())

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&projectProgress)
	err = self.db.Put(key, buf.Bytes(), &levelDbWriteOptions)
	return
}

// ---

func projectKey(project types.Project) string {
	return projectKeyById(project.Id)
}

func projectKeyById(id types.ProjectId) string {
	return fmt.Sprintf("%s:%s", projectsKey, id)
}

func projectNameIndexKey(name string) string {
	return fmt.Sprintf("ProjectNameToId:%s")
}

func projectRunsKeyForProject(project types.Project) string {
	return fmt.Sprintf("ProjectRunsForProjectId:%s:", project.Id)
}

func projectRunKey(projectRun types.ProjectRun) string {
	return projectRunKeyById(projectRun.Id)
}

func projectRunKeyById(id types.ProjectRunId) string {
	return fmt.Sprintf("%s:%s", projectRunsKey, id)
}

func projectRunForProjectKey(projectRun types.ProjectRun) string {
	return projectRunsKeyForProject(projectRun.Project) + projectRun.StartedAt.UTC().String()
}

func projectProgressPrefix(projectRun types.ProjectRun) string {
	return projectProgressPrefixByProjectRunId(projectRun.Id)
}

func projectProgressPrefixByProjectRunId(id types.ProjectRunId) string {
	return fmt.Sprintf("ProjectProgressForProjectRunId:%s:", id)
}
