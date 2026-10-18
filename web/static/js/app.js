(function () {
  "use strict";

  function applyTheme(theme) {
    document.documentElement.setAttribute("data-theme", theme);
  }
  try {
    var saved = localStorage.getItem("errgotrack-theme");
    if (saved) applyTheme(saved);
  } catch (e) {}

  window.toggleTheme = function () {
    var current = document.documentElement.getAttribute("data-theme") ||
      (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
    var next = current === "dark" ? "light" : "dark";
    applyTheme(next);
    try { localStorage.setItem("errgotrack-theme", next); } catch (e) {}
  };

  window.toggleSidebar = function () {
    var el = document.querySelector(".sidebar");
    if (el) el.classList.toggle("open");
  };

  // Renders a Chart.js chart from a <script type="application/json"> data
  // island next to a <canvas>, so server-rendered pages stay simple html/template
  // output and charting is opt-in per element.
  function initCharts() {
    document.querySelectorAll("canvas[data-chart]").forEach(function (canvas) {
      if (canvas.dataset.initialized) return;
      var dataEl = document.getElementById(canvas.dataset.chart);
      if (!dataEl) return;
      var config = JSON.parse(dataEl.textContent);
      new Chart(canvas.getContext("2d"), config);
      canvas.dataset.initialized = "1";
    });
  }

  document.addEventListener("DOMContentLoaded", initCharts);
  document.body && document.body.addEventListener("htmx:afterSwap", initCharts);
  document.addEventListener("htmx:afterSwap", initCharts);
})();
