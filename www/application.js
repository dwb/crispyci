(function () {

var apiPathPrefix = "/api/v1";

var defaultDateFormat = function (dt) {
  var m = moment(dt);
  return m.format('llll') + ' (' + m.fromNow() + ')';
};

var projectRunStatuses = {
  0: "Unknown",
  1: "Started",
  2: "Success",
  3: "Failure",
  4: "Aborted",
}

var projectRunStatusBootstrapTypes = {
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
  ready: function () {
    setInterval(function () {
      CrispyCI.set('currentTime', new Date())
    }, 1000);
  }
})

// --- Models ---

CrispyCI.ProjectRun = DS.Model.extend({
  status: DS.attr(),
  startedAt: DS.attr('date'),
  finishedAt: DS.attr('date'),
  project: DS.belongsTo('project', {async: true}),
  isBuilding: function () {
    return this.get('status') == 1;
  }.property('status'),
});

CrispyCI.Project = DS.Model.extend({
  name: DS.attr(),
  scriptSet: DS.attr(),
  projectRuns: DS.hasMany('projectRun'),
});

// --- Routes ----

CrispyCI.Router.map(function () {
  this.resource('projects', function () {
    this.route('new');
  });
  this.resource('project', {path: "/projects/:project_id"});
  this.resource('projectRun', {path: "/projectRuns/:project_run_id"});
});

if (Modernizr.history) {
  CrispyCI.Router.reopen({
    location: 'history'
  });
}

CrispyCI.IndexRoute = Ember.Route.extend({
  beforeModel: function () {
    this.transitionTo('projects');
  }
});

CrispyCI.ProjectsRoute = Ember.Route.extend({
  model: function (params) {
    return this.store.find('project');
  },
  setupController: function(controller, model) {
    controller.set('model', model);
  },

  sortProperties: ['name']
});

CrispyCI.ProjectRoute = Ember.Route.extend({
  model: function (params) {
    return this.store.find('project', params.project_id)
  },
  setupController: function(controller, model) {
    controller.set('model', model);
    controller.set('controllers.projectRuns.content', model.get('projectRuns'));
  }
});

CrispyCI.ProjectRunRoute = Ember.Route.extend({
  model: function (params) {
    return this.store.find('projectRun', params.project_run_id)
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
    this.listenForProjectRunUpdates();
    return this._super()
  },

  listenForProjectRunUpdates: function () {
    var store = this.get('store');
    var ws = newWebSocket(apiPathPrefix + "/projectRuns/updates");

    console.log("Connecting for project run updates...");
    var self = this;
    var retry = function () { self.listenForProjectRunUpdates() };

    ws.onopen = function () {
      console.log("Listening for project run updates...");
    };

    ws.onclose = function () {
      console.log("Server closed project run update connection. Retrying in 2s...");
      setTimeout(retry, 2000);
    };

    ws.onmessage = function (e) {
      var payload = JSON.parse(e.data);
      var id = payload.projectRun.id;
      var projectRun = store.recordForId('projectRun', id);

      if (payload.deleted) {
        projectRun.unloadRecord();
      } else {
        var projectId = payload.projectRun.project;
        payload.projectRuns = [payload.projectRun];
        delete payload.projectRun;
        delete payload.deleted;

        store.pushPayload('projectRun', payload);

        var project = store.getById('project', projectId);
        if (project) {
          var projectRuns = project.get('projectRuns');
          if (!projectRuns.contains(projectRun)) {
            projectRuns.pushObject(projectRun);
          }
        }
      }
    };
  }
});

CrispyCI.ProjectsController = Ember.ArrayController.extend({
  itemController: 'project'
});

CrispyCI.ProjectController = Ember.ObjectController.extend({
  needs: ['projectRuns'],

  lastProjectRun: function () {
    return CrispyCI.ProjectRunController.create({
      content: this.get('projectRuns.lastObject')
    });
  }.property('projectRuns.lastObject')
});


CrispyCI.ProjectRunsController = Ember.ArrayController.extend({
  itemController: 'projectRun',
  sortProperties: ['startedAt'],
  sortAscending: false
});

CrispyCI.ProjectRunController = Ember.ObjectController.extend({
  actions: {
    abort: function () {
      console.log("Abort project run " + this.get('model.id'));
      Ember.$.post(apiPathPrefix + "/projectRuns/" + this.get('model.id') + "/abort");
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
  }.property('model.startedAt', 'CrispyCI.currentTime'),

  finishedAt: function () {
    if (this.get('model.status') == 1) {
      return "";
    } else {
      return defaultDateFormat(this.get('model.finishedAt'));
    }
  }.property('model.finishedAt', 'model.status', 'CrispyCI.currentTime'),

  duration: function () {
    var d = this.get('model.finishedAt') - this.get('model.startedAt');
    if (d <= 0) {
      return "";
    } else {
      return moment.duration(d).humanize();
    }
  }.property('model.startedAt', 'model.finishedAt'),

  statusName: function () {
    return projectRunStatuses[this.get('status')];
  }.property('status'),

  statusBootstrapType: function () {
    return projectRunStatusBootstrapTypes[this.get('statusName')];
  }.property('statusName'),

  connectProgress: function () {
    var projectRun = this.get('model');
    var projectRunId = projectRun.get('id');
    var ws = newWebSocket(apiPathPrefix + "/projectRuns/" + projectRunId + "/progress");
    this.progressWs = ws

    console.log("Connecting for project run " + projectRunId + " progress...");

    ws.onopen = function () {
      console.log("Connected for project run " + projectRunId + " progress");
    };

    ws.onclose = function () {
      console.log("Server closed project run " + projectRunId + " progress connection.");
    };

    ws.onmessage = function (e) {
      Ember.$('#projectRunProgress').append(e.data);
      if (projectRun.get('isBuilding')) {
        var body = Ember.$('body');
        body.scrollTop(body.height());
      }
    };
  },

  disconnectProgress: function() {
    if (typeof this.progressWs !== "undefined") {
      var projectRun = this.get('model');
      var projectRunId = projectRun.get('id');
      console.log("Closing project run " + projectRunId + " progress connection.")
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

CrispyCI.ProjectSerializer = DS.RESTSerializer.extend({
  extractSingle: function(store, type, payload, id, requestType) {
    payload.projectRuns = payload.project.projectRuns;
    payload.project.projectRuns = payload.projectRuns.mapProperty('id');
    return this._super.apply(this, arguments);
  },

  extractArray: function(store, type, payload, id, requestType) {
    projectRuns = [];
    payload.projects.forEach(function (project) {
      projectRuns = projectRuns.concat(project.projectRuns);
      project.projectRuns = project.projectRuns.mapProperty('id');
    });

    payload.projectRuns = projectRuns;
    return this._super.apply(this, arguments);
  }
});

})()
