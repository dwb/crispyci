(function () {

var defaultDateFormat = function (dt) {
  var m = moment(dt);
  return m.format('llll') + ' (' + m.fromNow() + ')';
};

var jobStatuses = {
  0: "Unknown",
  1: "Started",
  2: "Success",
  3: "Failure"
}

var jobStatusBootstrapTypes = {
  Started: "started",
  Success: "success",
  Failure: "danger"
}

window.CrispyCI = Ember.Application.create({
})

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
  },

  sortProperties: ['name']
});

CrispyCI.JobsController = Ember.ArrayController.extend({
  itemController: 'job'
});

CrispyCI.JobController = Ember.ObjectController.extend({
  needs: ['jobRuns'],

  lastJobRun: function () {
    return CrispyCI.JobRunController.create({
      content: this.get('jobRuns.lastObject')
    });
  }.property('jobRuns.lastObject')
});


CrispyCI.JobRunsController = Ember.ArrayController.extend({
  itemController: 'jobRun',
  sortProperties: ['startedAt'],
  sortAscending: false
});

CrispyCI.JobRunController = Ember.ObjectController.extend({
  startedAt: function () {
    return defaultDateFormat(this.get('model.startedAt'));
  }.property('model.startedAt'),

  finishedAt: function () {
    if (this.get('model.status') == 1) {
      return "";
    } else {
      return defaultDateFormat(this.get('model.finishedAt'));
    }
  }.property('model.finishedAt', 'model.status'),

  duration: function () {
    var d = this.get('model.finishedAt') - this.get('model.startedAt');
    if (d <= 0) {
      return "";
    } else {
      return moment.duration(d).humanize();
    }
  }.property('model.startedAt', 'model.finishedAt'),

  statusName: function () {
    return jobStatuses[this.get('status')];
  }.property('status'),

  statusBootstrapType: function () {
    return jobStatusBootstrapTypes[this.get('statusName')];
  }.property('statusName')
});

CrispyCI.JobRoute = Ember.Route.extend({
  model: function (params) {
    return this.store.find('job', params.job_id)
  },
  setupController: function(controller, model) {
    controller.set('model', model);
    controller.set('controllers.jobRuns.content', model.get('jobRuns'));
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
  job: DS.belongsTo('job'),
});

CrispyCI.Job = DS.Model.extend({
  name: DS.attr(),
  scriptSet: DS.attr(),
  jobRuns: DS.hasMany(CrispyCI.JobRun)
});

})()
