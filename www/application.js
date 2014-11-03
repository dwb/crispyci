(function () {

Ember.DSModelRoute = Ember.Route.extend({
  deactivate: function() {
    var model = this.get('controller.model');
    model.rollback();
    if (model.get('isNew')) {
      model.deleteRecord();
    }
  },

  actions: {
    willTransition: function(transition) {
      var model = this.get('controller.model');
      if (model.get('isDirty')) {
        if (confirm('You have unsaved changes. They will be lost if you continue!')) {
          model.rollback();
        } else {
          transition.abort();
        }
      }
    }
  }
});

var __hasProp = {}.hasOwnProperty;

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
  url: DS.attr(),
  scriptSet: DS.attr(),
  projectBuilds: DS.hasMany('projectBuild'),
});

// --- Routes ----

CrispyCI.Router.map(function () {
  this.resource('projects', function () {
    this.route('new');
  });
  this.resource('project', {path: "/projects/:project_id"}, function () {
    this.route('edit');
  });
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

CrispyCI.ProjectRoute = Ember.DSModelRoute.extend({
  model: function (params) {
    return this.store.find('project', params.project_id)
  },

  setupController: function(controller, model) {
    controller.set('model', model);
  }
});

CrispyCI.ProjectIndexRoute = Ember.Route.extend({
  setupController: function(controller, model) {
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

CrispyCI.ProjectsNewRoute = Ember.DSModelRoute.extend({
  controllerName: "projectEdit",
  templateName: "project/edit",

  model: function (params) {
    return this.store.createRecord('project');
  },

  setupController: function(controller, model) {
    controller.set("model", model);
  }
});

CrispyCI.ProjectEditRoute = Ember.DSModelRoute.extend({
  renderTemplate: function () {
    this.render("project/edit", {controller: "projectEdit"});
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
});

CrispyCI.ProjectIndexController = Ember.ObjectController.extend({
  needs: ['projectBuilds'],

  lastProjectBuild: function () {
    return CrispyCI.ProjectBuildController.create({
      content: this.get('projectBuilds.lastObject')
    });
  }.property('projectBuilds.lastObject'),

  actions : {
    delete: function () {
      if (confirm("Are you sure you want to delete this project?")) {
        var self = this;
        this.get('model').destroyRecord().then(function () {
          self.transitionToRoute('projects');
        }, function (error) {
          alert("Couldn't delete for some reason, sorry");
        });
      }
    }
  }
});

CrispyCI.ProjectEditController = Ember.ObjectController.extend({
  actions: {
    save: function () {
      var self = this;
      this.get('model').save().then(function (model) {
        // Success
        self.transitionToRoute('projects');
      }, function (error) {
        // Error
        return error;
      });
    },
  },
});

CrispyCI.ProjectBuildsController = Ember.ArrayController.extend({
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

  ajaxError: function(jqXHR) {
    var error = this._super(jqXHR);

    if (jqXHR && jqXHR.status === 422) {
      var response = Ember.$.parseJSON(jqXHR.responseText),
          errors = {};

      if (response.errors !== undefined) {
        var jsonErrors = response.errors;

        Ember.EnumerableUtils.forEach(jsonErrors, function(error) {
          var key = Ember.String.camelize(error['field']);
          var msg = error['message'];
          if (errors[key] === undefined) {
            errors[key] = [];
          }
          errors[key] = errors[key].concat(msg)
        });
      }
      return new DS.InvalidError(errors);
    } else {
      return error;
    }
  }
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

/* Form components lifted and very slightly modified from
 * http://alexspeller.com/server-side-validations-with-ember-data-and-ds-errors/
 */

DS.Model.reopen({
  adapterDidInvalidate: function(errors) {
    var errorValue, key, recordErrors, _results;
    recordErrors = this.get('errors');
    _results = [];
    for (key in errors) {
      if (!__hasProp.call(errors, key)) continue;
      errorValue = errors[key];
      _results.push(recordErrors.add(key, errorValue));
    }
    return _results;
  }
});

Ember.Handlebars.helper('titleize', function(text) {
  return text.titleize();
});

String.prototype.titleize = function() {
  return this.underscore().replace(/_/g, " ").capitalize();
};

CrispyCI.ErrorMessagesComponent = Em.Component.extend({
  errors: Em.computed.filter('for.errors.content', function(error) {
    return error.attribute !== 'base';
  }),
  baseErrors: Em.computed.filter('for.errors.content', function(error) {
    return error.attribute === 'base';
  })
});

CrispyCI.ObjectFormComponent = Em.Component.extend({
  buttonLabel: "Submit",
  actions: {
    submit: function() {
      return this.sendAction();
    }
  }
});

CrispyCI.FormFieldComponent = Em.Component.extend({
  type: Em.computed('for', function() {
    if (this.get('for').match(/password/)) {
      return "password";
    } else {
      return "text";
    }
  }),

  label: Em.computed('for', 'labelText', function() {
    var labelText = this.get('labelText');
    if (labelText !== undefined) {
      return labelText;
    } else {
      return this.get('for').underscore().replace(/_/g, " ").capitalize();
    }
  }),

  fieldId: Em.computed('object', 'for', function() {
    return "" + (Em.guidFor(this.get('object'))) + "-input-" + (this.get('for'));
  }),

  object: Em.computed.alias('parentView.for'),

  hasError: (function() {
    var _ref;
    return (_ref = this.get('object.errors')) != null ? _ref.has(this.get('for')) : void 0;
  }).property('object.errors.[]'),

  errors: (function() {
    if (!this.get('object.errors')) {
      return Em.A();
    }
    return this.get('object.errors').errorsFor(this.get('for')).mapBy('message').join(', ');
  }).property('object.errors.[]'),

  setupBindings: (function() {
    var _ref;
    if ((_ref = this.binding) != null) {
      _ref.disconnect(this);
    }
    this.binding = Em.Binding.from("object." + (this.get('for'))).to('value');
    return this.binding.connect(this);
  }).on('init').observes('for', 'object'),

  tearDownBindings: (function() {
    var _ref;
    return (_ref = this.binding) != null ? _ref.disconnect(this) : void 0;
  }).on('willDestroyElement')
});

})()
