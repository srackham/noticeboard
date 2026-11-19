// Client behaviour for the noticeboard (DESIGN.md sections 5.2 and 6.5).
// It runs with no inline scripts, as the Content-Security-Policy requires.
(function () {
  "use strict";

  // isBoardRequest reports whether an HTMX event belongs to the board poll.
  function isBoardRequest(detail) {
    if (!detail) {
      return false;
    }
    if (detail.requestConfig && detail.requestConfig.path === "/board") {
      return true;
    }
    return Boolean(detail.elt && detail.elt.id === "board");
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

  // A failed poll keeps the last good board on screen and shows STALE.
  document.body.addEventListener("htmx:responseError", function (evt) {
    if (isBoardRequest(evt.detail)) {
      setStale(true);
    }
  });
  document.body.addEventListener("htmx:sendError", function (evt) {
    if (isBoardRequest(evt.detail)) {
      setStale(true);
    }
  });

  // A successful poll swap clears the indicator.
  document.body.addEventListener("htmx:afterSwap", function (evt) {
    if (isBoardRequest(evt.detail)) {
      setStale(false);
    }
  });

  // The X beside a table search box clears it and reloads the table without
  // the filter. Submitting the enclosing HTMX form re-renders the grid.
  document.body.addEventListener("click", function (evt) {
    var target = evt.target;
    var clear = target && target.closest && target.closest("[data-clear-search]");
    if (!clear) {
      return;
    }
    var form = clear.closest("form");
    if (!form) {
      return;
    }
    var input = form.querySelector('input[name="q"]');
    if (input) {
      input.value = "";
    }
    if (form.requestSubmit) {
      form.requestSubmit();
    }
  });

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
