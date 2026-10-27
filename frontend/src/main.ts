import "./app.css";
import "@fontsource-variable/oxanium";
import App from "./App.svelte";
import { mount } from "svelte";

const el = document.getElementById("app");
if (el) {
  mount(App, { target: el });
}
