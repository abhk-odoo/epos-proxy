<template>
    <KioskView v-if="kioskState.url" :url="kioskState.url" :reload-trigger="kioskState.reloadTrigger" @clear-url="kioskState.url = ''" />
    <PrinterList v-else />
</template>

<script setup>
import { kioskState } from './main.js';
import PrinterList from './printer-list.vue';
import KioskView from './kiosk-view.vue';
</script>