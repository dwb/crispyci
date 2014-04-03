(function () {

var apiPathPrefix = "/api/v1";

var defaultDateFormat = function (dt) {
  var m = moment(dt);
  return m.format('llll') + ' (' + m.fromNow() + ')';
};

var projectBuildStatuses = {
  0: "Unknown",
  1: "Started",
  2: "Success",
  3: "Failure",
  4: "Aborted",
}

var projectBuildStatusBootstrapTypes = {
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

CrispyCI.ProjectBuild = DS.Model.extend({
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
  projectBuilds: DS.hasMany('projectBuild'),
});

// --- Routes ----

CrispyCI.Router.map(function () {
  this.resource('projects', function () {
    this.route('new');
  });
  this.resource('project', {path: "/projects/:project_id"});
  this.resource('projectBuild', {path: "/projectBuilds/:project_build_id"});
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
    controller.set('controllers.projectBuilds.content', model.get('projectBuilds'));
  }
});

CrispyCI.ProjectBuildRoute = Ember.Route.extend({
  model: function (params) {
    return this.store.find('projectBuild', params.project_build_id)
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
    this.listenForProjectBuildUpdates();
    return this._super()
  },

  listenForProjectBuildUpdates: function () {
    var store = this.get('store');
    var ws = newWebSocket(apiPathPrefix + "/projectBuilds/updates");

    console.log("Connecting for project build updates...");
    var self = this;
    var retry = function () { self.listenForProjectBuildUpdates() };

    ws.onopen = function () {
      console.log("Listening for project build updates...");
    };

    ws.onclose = function () {
      console.log("Server closed project build update connection. Retrying in 2s...");
      setTimeout(retry, 2000);
    };

    ws.onmessage = function (e) {
      var payload = JSON.parse(e.data);
      var id = payload.projectBuild.id;
      var projectBuild = store.recordForId('projectBuild', id);

      if (payload.deleted) {
        projectBuild.unloadRecord();
      } else {
        var projectId = payload.projectBuild.project;
        payload.projectBuilds = [payload.projectBuild];
        delete payload.projectBuild;
        delete payload.deleted;

        store.pushPayload('projectBuild', payload);

        var project = store.getById('project', projectId);
        if (project) {
          var projectBuilds = project.get('projectBuilds');
          if (!projectBuilds.contains(projectBuild)) {
            projectBuilds.pushObject(projectBuild);
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
  needs: ['projectBuilds'],

  lastProjectBuild: function () {
    return CrispyCI.ProjectBuildController.create({
      content: this.get('projectBuilds.lastObject')
    });
  }.property('projectBuilds.lastObject')
});


CrispyCI.ProjectBuilds = Ember.ArrayController.extend({
  itemController: 'projectBuild',
  sortProperties: ['startedAt'],
  sortAscending: false
});

CrispyCI.ProjectBuildController = Ember.ObjectController.extend({
  actions: {
    abort: function () {
      console.log("Abort project build " + this.get('model.id'));
      Ember.$.post(apiPathPrefix + "/projectBuilds/" + this.get('model.id') + "/abort");
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
    return projectBuildStatuses[this.get('status')];
  }.property('status'),

  statusBootstrapType: function () {
    return projectBuildStatusBootstrapTypes[this.get('statusName')];
  }.property('statusName'),

  connectProgress: function () {
    var projectBuild = this.get('model');
    var projectBuildId = projectBuild.get('id');
    var ws = newWebSocket(apiPathPrefix + "/projectBuilds/" + projectBuildId + "/progress");
    this.progressWs = ws

    console.log("Connecting for project build " + projectBuildId + " progress...");

    ws.onopen = function () {
      console.log("Connected for project build " + projectBuildId + " progress");
    };

    ws.onclose = function () {
      console.log("Server closed project build " + projectBuildId + " progress connection.");
    };

    ws.onmessage = function (e) {
      Ember.$('#projectBuildProgress').append(e.data);
      if (projectBuild.get('isBuilding')) {
        var body = Ember.$('body');
        body.scrollTop(body.height());
      }
    };
  },

  disconnectProgress: function() {
    if (typeof this.progressWs !== "undefined") {
      var projectBuild = this.get('model');
      var projectBuildId = projectBuild.get('id');
      console.log("Closing project build " + projectBuildId + " progress connection.")
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
    payload.projectBuilds = payload.project.projectBuilds;
    payload.project.projectBuilds = payload.projectBuilds.mapProperty('id');
    return this._super.apply(this, arguments);
  },

  extractArray: function(store, type, payload, id, requestType) {
    projectBuilds = [];
    payload.projects.forEach(function (project) {
      projectBuilds = projectBuilds.concat(project.projectBuilds);
      project.projectBuilds = project.projectBuilds.mapProperty('id');
    });

    payload.projectBuilds = projectBuilds;
    return this._super.apply(this, arguments);
  }
});

})()
