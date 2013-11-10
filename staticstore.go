package main

type StaticStore struct{}

func (self *StaticStore) Init(_ string) error {
	return nil
}

func (self *StaticStore) AllJobs() (out []Job, err error) {
	out = []Job{Job{0, "test-job", "test"}}
	return
}

func (self *StaticStore) JobByName(name string) (out *Job, err error) {
	all, err := self.AllJobs()
	if err != nil {
		return
	}

	for _, job := range all {
		if job.Name == name {
			return &job, nil
		}
	}

	return nil, nil
}
