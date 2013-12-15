(function () {

var apiPathPrefix = "/api/v1";

var defaultDateFormat = function (dt) {
  var m = moment(dt);
  return m.format('llll') + ' (' + m.fromNow() + ')';
};

var jobStatuses = {
  0: "Unknown",
  1: "Started",
  2: "Success",
  3: "Failure",
  4: "Aborted",
}

var jobStatusBootstrapTypes = {
  Started: "started",
  Success: "success",
  Failure: "danger",
  Aborted: "danger",
}

var newWebSocket = function (path) {
  var loc = window.location, url;
  if (loc.protocol == "https:") {
    url = "wss://"
  } else {
    url = "ws://"
  }
  url += loc.host

  return new WebSocket(url + path)
}


window.CrispyCI = Ember.Application.create({
})

// --- Models ---

CrispyCI.JobRun = DS.Model.extend({
  status: DS.attr(),
  startedAt: DS.attr('date'),
  finishedAt: DS.attr('date'),
  job: DS.belongsTo('job', {async: true}),
  isRunning: function () {
    return this.get('status') == 1;
  }.property('status'),
});

CrispyCI.Job = DS.Model.extend({
  name: DS.attr(),
  scriptSet: DS.attr(),
  jobRuns: DS.hasMany('jobRun'),
});

// --- Routes ----

CrispyCI.Router.map(function () {
  this.resource('jobs', function () {
    this.route('new');
  });
  this.resource('job', {path: "/jobs/:job_id"});
  this.resource('jobRun', {path: "/jobRuns/:job_run_id"});
});

if (Modernizr.history) {
  CrispyCI.Router.reopen({
    location: 'history'
  });
}

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

CrispyCI.JobRoute = Ember.Route.extend({
  model: function (params) {
    return this.store.find('job', params.job_id)
  },
  setupController: function(controller, model) {
    controller.set('model', model);
    controller.set('controllers.jobRuns.content', model.get('jobRuns'));
  }
});

CrispyCI.JobRunRoute = Ember.Route.extend({
  model: function (params) {
    return this.store.find('jobRun', params.job_run_id)
  },

  setupController: function(controller, model) {
    controller.set('model', model);
    controller.connectProgress();
  },

  exit: function () {
    this.get('controller').disconnectProgress();
  }
});

// --- Controllers ---

CrispyCI.ApplicationController = Ember.Controller.extend({
  init: function () {
    this.listenForJobRunUpdates();
    return this._super()
  },

  listenForJobRunUpdates: function () {
    var store = this.get('store');
    var ws = newWebSocket(apiPathPrefix + "/jobRuns/updates");

    console.log("Connecting for job run updates...");
    var self = this;
    var retry = function () { self.listenForJobRunUpdates() };

    ws.onopen = function () {
      console.log("Listening for job run updates...");
    };

    ws.onclose = function () {
      console.log("Server closed job run update connection. Retrying in 2s...");
      setTimeout(retry, 2000);
    };

    ws.onmessage = function (e) {
      var payload = JSON.parse(e.data);
      var id = payload.jobRun.id;
      var jobRun = store.recordForId('jobRun', id);

      if (payload.deleted) {
        jobRun.unloadRecord();
      } else {
        var jobId = payload.jobRun.job;
        payload.jobRuns = [payload.jobRun];
        delete payload.jobRun;
        delete payload.deleted;

        store.pushPayload('jobRun', payload);

        var job = store.getById('job', jobId);
        if (job) {
          var jobRuns = job.get('jobRuns');
          if (!jobRuns.contains(jobRun)) {
            jobRuns.pushObject(jobRun);
          }
        }
      }
    };
  }
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
  actions: {
    abort: function () {
      console.log("Abort job run " + this.get('model.id'));
      Ember.$.post(apiPathPrefix + "/jobRuns/" + this.get('model.id') + "/abort");
      this.set('aborting', true);

      return false;
    },
  },

  aborting: false,

  abortButtonText: function () {
    return this.get('aborting') ? "Aborting..." : "Abort"
  }.property('aborting'),

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
  }.property('statusName'),

  connectProgress: function () {
    var jobRun = this.get('model');
    var jobRunId = jobRun.get('id');
    var ws = newWebSocket(apiPathPrefix + "/jobRuns/" + jobRunId + "/progress");
    this.progressWs = ws

    console.log("Connecting for job run " + jobRunId + " progress...");

    ws.onopen = function () {
      console.log("Connected for job run " + jobRunId + " progress");
    };

    ws.onclose = function () {
      console.log("Server closed job run " + jobRunId + " progress connection.");
    };

    ws.onmessage = function (e) {
      Ember.$('#jobRunProgress').append(e.data);
      if (jobRun.get('isRunning')) {
        var body = Ember.$('body');
        body.scrollTop(body.height());
      }
    };
  },

  disconnectProgress: function() {
    if (typeof this.progressWs !== "undefined") {
      var jobRun = this.get('model');
      var jobRunId = jobRun.get('id');
      console.log("Closing job run " + jobRunId + " progress connection.")
      this.progressWs.close();
      this.progressWs = undefined;
    }
  },

  willDestroy: function () {
    this.disconnectProgress();
  },
});

// --- REST interfaces ---

CrispyCI.ApplicationAdapter = DS.RESTAdapter.extend({
  namespace: apiPathPrefix.replace(/^\//, ''),
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

})()
