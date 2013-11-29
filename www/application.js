window.CrispyCI = Ember.Application.create({
})

CrispyCI.JobStatuses = {
  0: "Unknown",
  1: "Started",
  2: "Success",
  3: "Failure"
}

CrispyCI.JobStatusBootstrapTypes = {
  Started: "started",
  Success: "success",
  Failure: "danger"
}

CrispyCI.Router.map(function () {
  this.resource('jobs', function () {
    this.route('new');
  });
  this.resource('job', {path: "/jobs/:job_id"});
});

if (Modernizr.history) {
  CrispyCI.Router.reopen({
    location: 'history'
  });
}

CrispyCI.ApplicationController = Ember.Controller.extend({
  init: function () {
    this.listenForJobRunUpdates();
    return this._super()
  },

  listenForJobRunUpdates: function () {
    var store = this.get('store');
    var ws = new WebSocket("ws://localhost:3000/api/v1/job_runs/updates");

    ws.onopen = function () {
      console.log("Listening for job run updates...");
    };

    ws.onclose = function () {
      console.log("Server closed job run update connection");
    };

    ws.onmessage = function (e) {
      var payload = JSON.parse(e.data);
      var id = payload.jobRun.id;
      var jobId = payload.jobRun.job;
      payload.jobRuns = [payload.jobRun];
      delete payload.jobRun;

      store.pushPayload('jobRun', payload);

      var jobRun = store.recordForId('jobRun', id);
      var job = store.getById('job', jobId);
      if (job) {
        var jobRuns = job.get('jobRuns');
        if (!jobRuns.contains(jobRun)) {
          jobRuns.pushObject(jobRun);
        }
      }
    };
  }
});

CrispyCI.IndexRoute = Ember.Route.extend({
  beforeModel: function () {
    this.transitionTo('jobs');
  }
});

CrispyCI.JobsRoute = Ember.Route.extend({
  model: function (params) {
    return this.store.find('job');
  },
  setupController: function(controller, model) {
    controller.set('model', model);
  }
});

CrispyCI.JobController = Ember.ObjectController.extend({
  lastJobRun: function () {
    return this.get('jobRuns.lastObject');
  }.property('jobRuns.lastObject'),

  jobRunsReversed: function () {
    return this.get('jobRuns').slice(0).reverseObjects();
  }.property('jobRuns.firstObject')
});

CrispyCI.JobRoute = Ember.Route.extend({
  model: function (params) {
    return this.store.find('job', params.job_id)
  },
  setupController: function(controller, model) {
    controller.set('model', model);
  }
});

CrispyCI.ApplicationAdapter = DS.RESTAdapter.extend({
  namespace: 'api/v1'
});

CrispyCI.JobSerializer = DS.RESTSerializer.extend({
  extractSingle: function(store, type, payload, id, requestType) {
    payload.jobRuns = payload.job.jobRuns;
    payload.job.jobRuns = payload.jobRuns.mapProperty('id');
    return this._super.apply(this, arguments);
  },

  extractArray: function(store, type, payload, id, requestType) {
    jobRuns = [];
    payload.jobs.forEach(function (job) {
      jobRuns = jobRuns.concat(job.jobRuns);
      job.jobRuns = job.jobRuns.mapProperty('id');
    });

    payload.jobRuns = jobRuns;
    return this._super.apply(this, arguments);
  }
});

CrispyCI.JobRun = DS.Model.extend({
  status: DS.attr(),
  startedAt: DS.attr('date'),
  finishedAt: DS.attr('date'),
  duration: function() {
    return moment.duration(this.get('finishedAt') - this.get('startedAt')).humanize();
  }.property('startedAt', 'finishedAt'),

  job: DS.belongsTo('job'),

  statusName: function () {
    return CrispyCI.JobStatuses[this.get('status')];
  }.property('status'),

  statusBootstrapType: function () {
    return CrispyCI.JobStatusBootstrapTypes[this.get('statusName')];
  }.property('statusName')
});

CrispyCI.Job = DS.Model.extend({
  name: DS.attr(),
  scriptSet: DS.attr(),
  jobRuns: DS.hasMany(CrispyCI.JobRun)
});

