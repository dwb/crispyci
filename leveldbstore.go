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

	"github.com/dwb/crispyci/types"
)

type LevelDbStore struct {
	db *leveldb.DB
}

const projectsKey = "Project:"
const projectBuildsKey = "ProjectBuild:"

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

	dbRange := rangeForKeyPrefix(projectsKey)
	iter := self.db.NewIterator(&dbRange, nil)
	defer iter.Release()
	for iter.Next() {
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
		[]byte(fmt.Sprintf("%s", project.Id)))

	err = self.db.Write(batch, &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) ProjectBuildById(id types.ProjectBuildId) (projectBuild *types.ProjectBuild, err error) {
	buf, err := self.db.Get([]byte(projectBuildKeyById(id)), nil)
	if err != nil {
		return
	}

	dec := gob.NewDecoder(bytes.NewBuffer(buf))
	err = dec.Decode(&projectBuild)
	if err != nil {
		return nil, err
	}
	return
}

func (self *LevelDbStore) BuildsForProject(project types.Project) (results []types.ProjectBuild, err error) {
	results = make([]types.ProjectBuild, 0)

	ss, err := self.db.GetSnapshot()
	if err != nil {
		return
	}
	defer ss.Release()

	dbRange := rangeForKeyPrefix(projectBuildsKeyForProject(project))
	iter := ss.NewIterator(&dbRange, nil)
	defer iter.Release()
	for iter.Next() {
		var projectBuild types.ProjectBuild
		dec := gob.NewDecoder(bytes.NewBuffer(iter.Value()))
		err := dec.Decode(&projectBuild)
		if err == nil {
			results = append(results, projectBuild)
		}
	}
	return
}

func (self *LevelDbStore) LastBuildForProject(project types.Project) (result *types.ProjectBuild, err error) {
	// TODO: we can doubtless do this more efficiently if we don't decode
	// all previous results, but hey it'll be pretty quick as it is.

	results, err := self.BuildsForProject(project)
	if err != nil {
		return
	}

	if len(results) > 0 {
		return &results[len(results)-1], nil
	}

	return nil, nil
}

func (self *LevelDbStore) WriteProjectBuild(projectBuild types.ProjectBuild) (err error) {
	batch := new(leveldb.Batch)

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&projectBuild)
	toWrite := buf.Bytes()

	batch.Put([]byte(projectBuildKey(projectBuild)), toWrite)
	batch.Put([]byte(projectBuildForProjectKey(projectBuild)), toWrite)

	err = self.db.Write(batch, &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) DeleteProjectBuild(projectBuild types.ProjectBuild) (err error) {
	batch := new(leveldb.Batch)

	dbRange := rangeForKeyPrefix(projectProgressPrefix(projectBuild))
	iter := self.db.NewIterator(&dbRange, nil)
	defer iter.Release()
	for iter.Next() {
		batch.Delete(iter.Key())
	}

	batch.Delete([]byte(projectBuildForProjectKey(projectBuild)))
	batch.Delete([]byte(projectBuildKey(projectBuild)))

	err = self.db.Write(batch, &levelDbWriteOptions)
	return
}

func (self *LevelDbStore) ProgressForProjectBuild(projectBuild types.ProjectBuild) (progress *[]types.ProjectProgress, err error) {
	ps := make([]types.ProjectProgress, 0)

	dbRange := rangeForKeyPrefix(projectProgressPrefix(projectBuild))
	iter := self.db.NewIterator(&dbRange, nil)
	defer iter.Release()

	for iter.Next() {
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
	key := []byte(projectProgressPrefix(projectProgress.ProjectBuild) + projectProgress.Time.UTC().String())

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(&projectProgress)
	err = self.db.Put(key, buf.Bytes(), &levelDbWriteOptions)
	return
}

// ---

func rangeForKeyPrefix(prefix string) leveldbutil.Range {
	startKey := []byte(prefix)
	endKey := make([]byte, len(startKey))
	copy(endKey, startKey)
	endKey[len(endKey)-1]++
	return leveldbutil.Range{Start: startKey, Limit: endKey}
}

func projectKey(project types.Project) string {
	return projectKeyById(project.Id)
}

func projectKeyById(id types.ProjectId) string {
	return fmt.Sprintf("%s%s", projectsKey, id)
}

func projectNameIndexKey(name string) string {
	return fmt.Sprintf("ProjectNameToId:%s", name)
}

func projectBuildsKeyForProject(project types.Project) string {
	return fmt.Sprintf("ProjectBuildsForProjectId:%s:", project.Id)
}

func projectBuildKey(projectBuild types.ProjectBuild) string {
	return projectBuildKeyById(projectBuild.Id)
}

func projectBuildKeyById(id types.ProjectBuildId) string {
	return fmt.Sprintf("%:%s", projectBuildsKey, id)
}

func projectBuildForProjectKey(projectBuild types.ProjectBuild) string {
	return projectBuildsKeyForProject(projectBuild.Project) + projectBuild.StartedAt.UTC().String()
}

func projectProgressPrefix(projectBuild types.ProjectBuild) string {
	return projectProgressPrefixByProjectBuildId(projectBuild.Id)
}

func projectProgressPrefixByProjectBuildId(id types.ProjectBuildId) string {
	return fmt.Sprintf("ProjectProgressForProjectBuildId:%s:", id)
}
