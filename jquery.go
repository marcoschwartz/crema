package crema

// jQueryShim is a minimal jQuery implementation that maps jQuery's most-used
// methods to crema's native DOM APIs. Injected into the VM before page scripts
// run, so sites that depend on jQuery work without loading the full 90KB library.
//
// Covers: $(), jQuery(), $(document).ready(), selectors, DOM manipulation,
// CSS, classes, events, traversal, ajax (basic), and chaining.
const jQueryShim = `
(function(window) {
	"use strict";

	function jQueryObj(elements) {
		this.length = elements.length;
		for (var i = 0; i < elements.length; i++) {
			this[i] = elements[i];
		}
	}

	// Core iteration
	jQueryObj.prototype.each = function(fn) {
		for (var i = 0; i < this.length; i++) {
			fn.call(this[i], i, this[i]);
		}
		return this;
	};

	// DOM content
	jQueryObj.prototype.html = function(val) {
		if (val === undefined) {
			return this.length > 0 ? this[0].innerHTML : "";
		}
		this.each(function() { this.innerHTML = val; });
		return this;
	};
	jQueryObj.prototype.text = function(val) {
		if (val === undefined) {
			return this.length > 0 ? this[0].textContent : "";
		}
		this.each(function() { this.textContent = val; });
		return this;
	};
	jQueryObj.prototype.val = function(val) {
		if (val === undefined) {
			return this.length > 0 ? this[0].value : "";
		}
		this.each(function() { this.value = val; });
		return this;
	};

	// Attributes
	jQueryObj.prototype.attr = function(name, val) {
		if (val === undefined) {
			return this.length > 0 ? this[0].getAttribute(name) : null;
		}
		this.each(function() { this.setAttribute(name, val); });
		return this;
	};
	jQueryObj.prototype.removeAttr = function(name) {
		this.each(function() { this.removeAttribute(name); });
		return this;
	};
	jQueryObj.prototype.prop = function(name, val) {
		if (val === undefined) {
			return this.length > 0 ? this[0][name] : undefined;
		}
		this.each(function() { this[name] = val; });
		return this;
	};
	jQueryObj.prototype.data = function(name, val) {
		if (val === undefined) {
			return this.length > 0 ? this[0].getAttribute("data-" + name) : null;
		}
		this.each(function() { this.setAttribute("data-" + name, val); });
		return this;
	};

	// CSS classes
	jQueryObj.prototype.addClass = function(cls) {
		this.each(function() { this.classList.add(cls); });
		return this;
	};
	jQueryObj.prototype.removeClass = function(cls) {
		if (cls === undefined) {
			this.each(function() { this.className = ""; });
		} else {
			this.each(function() { this.classList.remove(cls); });
		}
		return this;
	};
	jQueryObj.prototype.toggleClass = function(cls) {
		this.each(function() { this.classList.toggle(cls); });
		return this;
	};
	jQueryObj.prototype.hasClass = function(cls) {
		return this.length > 0 ? this[0].classList.contains(cls) : false;
	};

	// CSS styles
	jQueryObj.prototype.css = function(prop, val) {
		if (typeof prop === "object") {
			var self = this;
			for (var k in prop) { self.css(k, prop[k]); }
			return this;
		}
		if (val === undefined) {
			return this.length > 0 ? this[0].style[prop] : "";
		}
		this.each(function() { this.style[prop] = val; });
		return this;
	};

	// Show / Hide
	jQueryObj.prototype.show = function() {
		this.each(function() { this.style.display = ""; });
		return this;
	};
	jQueryObj.prototype.hide = function() {
		this.each(function() { this.style.display = "none"; });
		return this;
	};
	jQueryObj.prototype.toggle = function() {
		this.each(function() {
			if (this.style.display === "none") {
				this.style.display = "";
			} else {
				this.style.display = "none";
			}
		});
		return this;
	};

	// Events
	jQueryObj.prototype.on = function(events, selectorOrFn, fn) {
		var callback = fn || selectorOrFn;
		var eventList = events.split(" ");
		this.each(function() {
			var el = this;
			for (var i = 0; i < eventList.length; i++) {
				el.addEventListener(eventList[i], callback);
			}
		});
		return this;
	};
	jQueryObj.prototype.off = function(events, fn) {
		var eventList = events.split(" ");
		this.each(function() {
			var el = this;
			for (var i = 0; i < eventList.length; i++) {
				el.removeEventListener(eventList[i], fn);
			}
		});
		return this;
	};
	jQueryObj.prototype.click = function(fn) {
		if (fn) { return this.on("click", fn); }
		return this;
	};
	jQueryObj.prototype.submit = function(fn) {
		if (fn) { return this.on("submit", fn); }
		return this;
	};
	jQueryObj.prototype.trigger = function(event) {
		this.each(function() {
			if (this.dispatchEvent) {
				this.dispatchEvent(event);
			}
		});
		return this;
	};

	// Traversal
	jQueryObj.prototype.find = function(sel) {
		var results = [];
		this.each(function() {
			var found = this.querySelectorAll(sel);
			for (var i = 0; i < found.length; i++) {
				results.push(found[i]);
			}
		});
		return new jQueryObj(results);
	};
	jQueryObj.prototype.parent = function() {
		var results = [];
		this.each(function() {
			if (this.parentNode) { results.push(this.parentNode); }
		});
		return new jQueryObj(results);
	};
	jQueryObj.prototype.children = function(sel) {
		var results = [];
		this.each(function() {
			var kids = this.children;
			for (var i = 0; i < kids.length; i++) {
				results.push(kids[i]);
			}
		});
		return new jQueryObj(results);
	};
	jQueryObj.prototype.siblings = function() {
		var results = [];
		this.each(function() {
			if (this.parentNode) {
				var kids = this.parentNode.children;
				for (var i = 0; i < kids.length; i++) {
					if (kids[i] !== this) { results.push(kids[i]); }
				}
			}
		});
		return new jQueryObj(results);
	};
	jQueryObj.prototype.first = function() {
		return new jQueryObj(this.length > 0 ? [this[0]] : []);
	};
	jQueryObj.prototype.last = function() {
		return new jQueryObj(this.length > 0 ? [this[this.length - 1]] : []);
	};
	jQueryObj.prototype.eq = function(i) {
		return new jQueryObj(i >= 0 && i < this.length ? [this[i]] : []);
	};
	jQueryObj.prototype.closest = function(sel) {
		var results = [];
		this.each(function() {
			var el = this;
			while (el) {
				if (el.matches && el.matches(sel)) { results.push(el); return; }
				el = el.parentNode;
			}
		});
		return new jQueryObj(results);
	};

	// Manipulation
	jQueryObj.prototype.append = function(content) {
		this.each(function() {
			if (typeof content === "string") {
				this.innerHTML = this.innerHTML + content;
			}
		});
		return this;
	};
	jQueryObj.prototype.prepend = function(content) {
		this.each(function() {
			if (typeof content === "string") {
				this.innerHTML = content + this.innerHTML;
			}
		});
		return this;
	};
	jQueryObj.prototype.remove = function() {
		this.each(function() {
			if (this.parentNode) { this.parentNode.removeChild(this); }
		});
		return this;
	};
	jQueryObj.prototype.empty = function() {
		this.each(function() { this.innerHTML = ""; });
		return this;
	};

	// Dimensions (basic stubs for layout queries)
	jQueryObj.prototype.width = function() { return 0; };
	jQueryObj.prototype.height = function() { return 0; };
	jQueryObj.prototype.offset = function() { return { top: 0, left: 0 }; };
	jQueryObj.prototype.position = function() { return { top: 0, left: 0 }; };
	jQueryObj.prototype.scrollTop = function(val) { return 0; };

	// Animation stubs (execute callback immediately)
	jQueryObj.prototype.animate = function(props, duration, callback) {
		if (typeof duration === "function") { callback = duration; }
		this.css(props);
		if (callback) { callback.call(this); }
		return this;
	};
	jQueryObj.prototype.fadeIn = function(duration, callback) {
		this.show();
		if (typeof duration === "function") { duration(); }
		else if (callback) { callback(); }
		return this;
	};
	jQueryObj.prototype.fadeOut = function(duration, callback) {
		this.hide();
		if (typeof duration === "function") { duration(); }
		else if (callback) { callback(); }
		return this;
	};
	jQueryObj.prototype.slideDown = function(duration, callback) { return this.fadeIn(duration, callback); };
	jQueryObj.prototype.slideUp = function(duration, callback) { return this.fadeOut(duration, callback); };
	jQueryObj.prototype.slideToggle = function(duration, callback) { return this.toggle(); };

	// Filtering
	jQueryObj.prototype.filter = function(sel) {
		var results = [];
		this.each(function() {
			if (typeof sel === "string") {
				if (this.matches && this.matches(sel)) { results.push(this); }
			} else if (typeof sel === "function") {
				if (sel.call(this)) { results.push(this); }
			}
		});
		return new jQueryObj(results);
	};
	jQueryObj.prototype.not = function(sel) {
		var results = [];
		this.each(function() {
			if (typeof sel === "string") {
				if (!this.matches || !this.matches(sel)) { results.push(this); }
			}
		});
		return new jQueryObj(results);
	};
	jQueryObj.prototype.is = function(sel) {
		for (var i = 0; i < this.length; i++) {
			if (this[i].matches && this[i].matches(sel)) { return true; }
		}
		return false;
	};

	// Map / get
	jQueryObj.prototype.map = function(fn) {
		var results = [];
		this.each(function(i) { results.push(fn.call(this, i, this)); });
		return results;
	};
	jQueryObj.prototype.get = function(i) {
		if (i === undefined) {
			var arr = [];
			this.each(function() { arr.push(this); });
			return arr;
		}
		return this[i];
	};

	// The main jQuery function
	function jQuery(selector, context) {
		// $(function) → document ready
		if (typeof selector === "function") {
			selector();
			return;
		}
		// $(DOMElement)
		if (selector && selector.nodeType) {
			return new jQueryObj([selector]);
		}
		// $(jQueryObj)
		if (selector && selector.length !== undefined && selector.each) {
			return selector;
		}
		// $("<html>") → skip HTML creation
		if (typeof selector === "string" && selector[0] === "<") {
			return new jQueryObj([]);
		}
		// $("selector")
		if (typeof selector === "string") {
			var root = (context && context.querySelectorAll) ? context : document;
			var found = root.querySelectorAll(selector);
			var arr = [];
			for (var i = 0; i < found.length; i++) {
				arr.push(found[i]);
			}
			return new jQueryObj(arr);
		}
		return new jQueryObj([]);
	}

	// Static methods
	jQuery.fn = jQueryObj.prototype;
	jQuery.each = function(obj, fn) {
		if (obj && obj.length !== undefined) {
			for (var i = 0; i < obj.length; i++) { fn(i, obj[i]); }
		} else if (obj) {
			for (var k in obj) { fn(k, obj[k]); }
		}
	};
	jQuery.extend = function(target) {
		for (var i = 1; i < arguments.length; i++) {
			var src = arguments[i];
			if (src) {
				for (var k in src) { target[k] = src[k]; }
			}
		}
		return target;
	};
	jQuery.isFunction = function(f) { return typeof f === "function"; };
	jQuery.isArray = function(a) { return Array.isArray(a); };
	jQuery.isPlainObject = function(o) { return typeof o === "object" && o !== null; };
	jQuery.trim = function(s) { return s ? s.trim() : ""; };
	jQuery.inArray = function(val, arr) { return arr ? arr.indexOf(val) : -1; };
	jQuery.noop = function() {};
	jQuery.now = function() { return Date.now(); };
	jQuery.type = function(obj) { return typeof obj; };
	jQuery.parseJSON = function(s) { return JSON.parse(s); };

	// Ajax (basic stub)
	jQuery.ajax = function(opts) {
		if (typeof opts === "string") { opts = { url: opts }; }
		var url = opts.url;
		var method = opts.method || opts.type || "GET";
		return fetch(url, { method: method, body: opts.data })
			.then(function(resp) { return resp.text(); })
			.then(function(text) {
				if (opts.dataType === "json") { text = JSON.parse(text); }
				if (opts.success) { opts.success(text); }
			})
			.catch(function(err) {
				if (opts.error) { opts.error(err); }
			});
	};
	jQuery.get = function(url, callback) {
		return jQuery.ajax({ url: url, success: callback });
	};
	jQuery.post = function(url, data, callback) {
		return jQuery.ajax({ url: url, method: "POST", data: data, success: callback });
	};
	jQuery.getJSON = function(url, callback) {
		return jQuery.ajax({ url: url, dataType: "json", success: callback });
	};

	// Deferred (minimal stub)
	jQuery.Deferred = function() {
		var d = {};
		d.resolve = function() {};
		d.reject = function() {};
		d.promise = function() { return d; };
		d.done = function(fn) { return d; };
		d.fail = function(fn) { return d; };
		d.always = function(fn) { return d; };
		d.then = function(fn) { return d; };
		return d;
	};

	// Make it global
	window.jQuery = jQuery;
	window.$ = jQuery;
	jQuery.fn.init = jQuery;

})(window);
`
