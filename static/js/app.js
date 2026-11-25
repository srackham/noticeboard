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

  // The search box is an <input type="search">, so the browser shows its own
  // clear (x) button. When that empties the box, reload the table without the
  // filter: submitting the HTMX search form re-renders the grid.
  document.body.addEventListener("input", function (evt) {
    var input = evt.target;
    if (!input || input.id !== "table-search-input" || input.value !== "") {
      return;
    }
    var form = input.closest("form");
    if (form && form.requestSubmit) {
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
