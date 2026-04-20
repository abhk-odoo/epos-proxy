import { EventsOn, WindowFullscreen, WindowSetAlwaysOnTop, WindowUnfullscreen } from '../wailsjs/runtime/runtime';
import './app.css';
import App from "./App.vue";
import { createApp, reactive } from 'vue';

export const kioskState = reactive({
    url: '',
    reloadTrigger: 0,
});

createApp(App).mount('#app');

EventsOn("kiosk_open", (url) => {
    kioskState.url = url;
    kioskState.reloadTrigger++;
    WindowFullscreen();
    WindowSetAlwaysOnTop(true);
});

EventsOn("kiosk_close", () => {
    WindowUnfullscreen();
    WindowSetAlwaysOnTop(false);
    kioskState.url = '';
    kioskState.reloadTrigger = 0;
});