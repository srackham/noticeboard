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

  // Validation failures come back as 422 with the re-rendered form. HTMX does
  // not swap 4xx responses by default, so enable the swap for them.
  document.body.addEventListener("htmx:beforeSwap", function (evt) {
    if (evt.detail.xhr && evt.detail.xhr.status === 422) {
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
})();
