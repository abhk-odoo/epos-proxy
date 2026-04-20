<template>
    <div class="kiosk-wrapper" @contextmenu.prevent>
        <div v-if="urlError" class="status-overlay error-overlay">
            <div class="status-content">
                <div class="status-icon">⚠</div>
                <h3>Invalid URL</h3>
                <p>{{ urlError }}</p>
                <p class="status-url">{{ url }}</p>
                <button class="back-btn" @click="goBack">← Back</button>
            </div>
        </div>

        <template v-else>
            <div v-if="timedOut" class="status-overlay error-overlay">
                <div class="status-content">
                    <div class="status-icon">⏱</div>
                    <h3>Page took too long to load</h3>
                    <p>The page did not respond within {{ LOAD_TIMEOUT_MS / 1000 }} seconds.</p>
                    <p class="status-url">{{ url }}</p>
                    <div class="btn-row">
                        <button class="back-btn" @click="goBack">← Back</button>
                        <button class="retry-btn" @click="retry">Try Again</button>
                    </div>
                </div>
            </div>

            <div v-else-if="loadError" class="status-overlay error-overlay">
                <div class="status-content">
                    <div class="status-icon">✕</div>
                    <h3>Failed to load page</h3>
                    <p>{{ loadError }}</p>
                    <p class="status-url">{{ url }}</p>
                    <div class="btn-row">
                        <button class="back-btn" @click="goBack">← Back</button>
                        <button class="retry-btn" @click="retry">Try Again</button>
                    </div>
                </div>
            </div>

            <div v-else-if="loading" class="status-overlay loading-overlay">
                <div class="loading-spinner"></div>
                <p>Loading...</p>
            </div>

            <iframe ref="frameRef" :src="url" :key="internalReloadTrigger" class="kiosk-frame" frameborder="0"
                allow="autoplay; fullscreen; camera; microphone; geolocation" @load="onLoad" @error="onError" />
        </template>
    </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';

const props = defineProps({
    url: { type: String, required: true },
    reloadTrigger: { type: Number, default: 0 },
});

const emit = defineEmits(['clear-url']);

const frameRef = ref(null);
const loading = ref(false);
const loadError = ref('');
const timedOut = ref(false);
const internalReloadTrigger = ref(0);

const LOAD_TIMEOUT_MS = 15_000;
let loadTimer = null;

const urlError = computed(() => {
    const raw = props.url;
    if (!raw?.trim()) return 'No URL provided.';
    try {
        const parsed = new URL(raw);
        if (!['http:', 'https:'].includes(parsed.protocol)) {
            return `Unsupported protocol "${parsed.protocol}". Only http and https are allowed.`;
        }
        if (!parsed.hostname) return 'URL is missing a valid hostname.';
    } catch {
        return 'The URL is malformed and cannot be loaded.';
    }
    return null;
});

function startLoadTimer() {
    clearLoadTimer();
    loadTimer = setTimeout(() => {
        if (loading.value) {
            loading.value = false;
            timedOut.value = true;
        }
    }, LOAD_TIMEOUT_MS);
}

function clearLoadTimer() {
    if (loadTimer) {
        clearTimeout(loadTimer);
        loadTimer = null;
    }
}

function onLoad() {
    clearLoadTimer();
    loading.value = false;
    timedOut.value = false;
    loadError.value = '';
}

function onError() {
    clearLoadTimer();
    loading.value = false;
    timedOut.value = false;
    loadError.value = 'The page could not be loaded. Please check the connection and try again.';
}

function retry() {
    timedOut.value = false;
    loadError.value = '';
    loading.value = true;
    internalReloadTrigger.value++;
    startLoadTimer();
    frameRef.value?.focus();
}

function goBack() {
    clearLoadTimer();
    timedOut.value = false;
    loadError.value = '';
    loading.value = false;
    emit('clear-url');
}

watch(
    () => props.url,
    () => {
        if (urlError.value) return;
        timedOut.value = false;
        loadError.value = '';
        loading.value = true;
        startLoadTimer();
        frameRef.value?.focus();
    }
);

watch(
    () => props.reloadTrigger,
    () => {
        if (urlError.value) return;
        retry();
    }
);

const BLOCKED_KEYS = new Set([
    'F1', 'F2', 'F3', 'F4', 'F5', 'F6', 'F7', 'F8', 'F9', 'F10', 'F11', 'F12', 'Escape',
]);

function blockKeys(e) {
    if (
        BLOCKED_KEYS.has(e.key) ||
        (e.altKey && e.key === 'F4') ||
        (e.ctrlKey && e.key === 'w') ||
        (e.ctrlKey && e.key === 'r') ||
        (e.metaKey && e.key === 'q') ||
        (e.altKey && e.key === 'Tab') ||
        (e.ctrlKey && e.altKey && e.key === 'Delete') ||
        (e.ctrlKey && e.shiftKey && e.key === 'Escape') ||
        e.key === 'Meta' ||
        e.key === 'ContextMenu'
    ) {
        e.preventDefault();
        e.stopPropagation();
    }
}

function blockContextMenu(e) { e.preventDefault(); }
function blockTouchHold(e) { e.preventDefault(); }

function preventFocusSteal(e) {
    if (frameRef.value && !frameRef.value.contains(e.target)) {
        frameRef.value.focus();
    }
}

onMounted(() => {
    if (!urlError.value) {
        loading.value = true;
        startLoadTimer();
        frameRef.value?.focus();
    }
    document.addEventListener('keydown', blockKeys, true);
    document.addEventListener('contextmenu', blockContextMenu, true);
    document.addEventListener('touchstart', blockTouchHold, { passive: false });
    document.addEventListener('touchend', blockTouchHold, { passive: false });
    document.addEventListener('focusin', preventFocusSteal);
});

onUnmounted(() => {
    clearLoadTimer();
    document.removeEventListener('keydown', blockKeys, true);
    document.removeEventListener('contextmenu', blockContextMenu, true);
    document.removeEventListener('touchstart', blockTouchHold);
    document.removeEventListener('touchend', blockTouchHold);
    document.removeEventListener('focusin', preventFocusSteal);
});
</script>

<style scoped>
.kiosk-wrapper {
    position: fixed;
    inset: 0;
    width: 100vw;
    height: 100vh;
    background: #000;
    z-index: 99999;
    overflow: hidden;
    touch-action: manipulation;
}

.kiosk-frame {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    border: none;
    display: block;
    touch-action: auto;
    pointer-events: all;
}

.status-overlay {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 10;
}

.loading-overlay {
    flex-direction: column;
    gap: 16px;
    background: #1a1a1a;
    color: #fff;
}

.error-overlay {
    background: #1a1a1a;
    color: #fff;
}

.status-content {
    text-align: center;
    padding: 32px;
    max-width: 420px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
}

.status-icon {
    font-size: 36px;
    margin-bottom: 8px;
    color: #ff6b6b;
    line-height: 1;
}

.status-content h3 {
    margin: 0;
    color: #ff6b6b;
    font-size: 18px;
}

.status-content p {
    margin: 0;
    color: #ccc;
    font-size: 14px;
}

.status-url {
    font-size: 11px !important;
    color: #666 !important;
    word-break: break-all;
    margin-top: 8px !important;
    font-family: monospace;
}

.loading-spinner {
    width: 48px;
    height: 48px;
    border: 4px solid #333;
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 1s linear infinite;
}

@keyframes spin {
    to {
        transform: rotate(360deg);
    }
}

.btn-row {
    display: flex;
    gap: 12px;
    margin-top: 16px;
}

.retry-btn,
.back-btn {
    padding: 10px 28px;
    border-radius: 6px;
    font-size: 14px;
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
}

.retry-btn {
    background: transparent;
    color: #fff;
    border: 1px solid #555;
}

.retry-btn:hover {
    border-color: #fff;
    background: rgba(255, 255, 255, 0.08);
}

.back-btn {
    background: transparent;
    color: #aaa;
    border: 1px solid #444;
}

.back-btn:hover {
    border-color: #aaa;
    background: rgba(255, 255, 255, 0.05);
    color: #fff;
}
</style>