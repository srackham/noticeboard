// Client behaviour for the noticeboard (DESIGN.md sections 5.2 and 6.5).
// It runs with no inline scripts, as the Content-Security-Policy requires.
(function () {
  "use strict";

  // isClocksRequest reports whether an HTMX event belongs to the clocks poll.
  function isClocksRequest(detail) {
    if (!detail) {
      return false;
    }
    if (detail.requestConfig && detail.requestConfig.path === "/clocks") {
      return true;
    }
    return Boolean(detail.elt && detail.elt.id === "clocks");
  }

  function setStale(on) {
    var indicator = document.getElementById("stale");
    if (indicator) {
      indicator.hidden = !on;
    }
  }

  // Validation failures (422) and stale records (404) come back as HTML
  // fragments. HTMX does not swap 4xx responses by default, so enable the swap
  // for them. Plain-text 404s, such as http.NotFound, are still not swapped.
  document.body.addEventListener("htmx:beforeSwap", function (evt) {
    var xhr = evt.detail.xhr;
    if (!xhr) {
      return;
    }
    var status = xhr.status;
    var type = xhr.getResponseHeader("Content-Type") || "";
    if ((status === 422 || status === 404) && type.indexOf("text/html") === 0) {
      evt.detail.shouldSwap = true;
      evt.detail.isError = false;
    }
  });

  // A failed poll keeps the last good clocks display on screen and shows STALE.
  document.body.addEventListener("htmx:responseError", function (evt) {
    if (isClocksRequest(evt.detail)) {
      setStale(true);
    }
  });
  document.body.addEventListener("htmx:sendError", function (evt) {
    if (isClocksRequest(evt.detail)) {
      setStale(true);
    }
  });

  // A successful poll swap clears the indicator.
  document.body.addEventListener("htmx:afterSwap", function (evt) {
    if (isClocksRequest(evt.detail)) {
      setStale(false);
    }
  });

  // The admin pages sign out after a pause in user activity, so a window left
  // open does not stay logged in. The pause is in seconds, in the body's
  // data-idle-timeout attribute; the login and clocks pages have no such
  // attribute, so they never time out here.
  var idleEvents = ["mousemove", "mousedown", "keydown", "wheel", "touchstart", "scroll"];

  function startIdleLogout(seconds) {
    var timer = null;

    function signOut() {
      // End the server session too: /admin/login sends a signed-in visitor back
      // to /admin, so a plain redirect would not log anyone out.
      fetch("/admin/logout", {
        method: "POST",
        credentials: "same-origin",
        headers: { "HX-Request": "true" }, // the server refuses admin POSTs without it
      })
        .catch(function () {})
        .then(function () {
          window.location.assign("/admin/login");
        });
    }

    function reset() {
      window.clearTimeout(timer);
      timer = window.setTimeout(signOut, seconds * 1000);
    }

    idleEvents.forEach(function (name) {
      document.addEventListener(name, reset, { passive: true });
    });
    reset();
  }

  var idleSeconds = Number(document.body.getAttribute("data-idle-timeout"));
  if (idleSeconds > 0) {
    startIdleLogout(idleSeconds);
  }

  // The Cancel button in a form empties its region. data-clear-form names the
  // region ID; without a value it means the dashboard's clock form.
  document.body.addEventListener("click", function (evt) {
    var target = evt.target;
    var trigger = target && target.closest && target.closest("[data-clear-form]");
    if (trigger) {
      var id = trigger.getAttribute("data-clear-form") || "clock-form";
      var region = document.getElementById(id);
      if (region) {
        region.innerHTML = "";
      }
    }
  });
})();
