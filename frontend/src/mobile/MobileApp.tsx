import { useCallback, useEffect, useState } from "react";
import {
  ApiError,
  KioskState,
  closeKiosk,
  getKioskState,
  openKiosk,
  reloadKiosk,
  setKioskPin,
  setKioskUrl,
} from "./api";

const STATE_POLL_MS = 5000;

/**
 * Mobile kiosk-management page, served at /kiosk. Deliberately scoped to
 * kiosk controls only — no printer management, autostart, logs, or other
 * desktop functionality. PIN is the only credential; there is no login
 * step, session, or token — every mutating action sends the PIN with the
 * request, same as the desktop PIN pad.
 */
export default function MobileApp() {
  const [state, setState] = useState<KioskState | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [urlInput, setUrlInput] = useState("");
  const [pin, setPin] = useState("");
  const [newPin, setNewPin] = useState("");
  const [newPinConfirm, setNewPinConfirm] = useState("");

  const [message, setMessage] = useState<{ text: string; isError: boolean } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const s = await getKioskState();
      setState(s);
      setLoadError(null);
      setUrlInput((current) => (current === "" ? s.url : current));
    } catch (err) {
      setLoadError(err instanceof ApiError ? err.message : "Failed to reach the kiosk machine.");
    }
  }, []);

  useEffect(() => {
    refresh();
    const id = window.setInterval(refresh, STATE_POLL_MS);
    return () => window.clearInterval(id);
  }, [refresh]);

  const runAction = async (
    name: string,
    action: () => Promise<KioskState>,
    successText: string,
  ): Promise<boolean> => {
    setBusy(name);
    setMessage(null);
    try {
      const result = await action();
      setState(result);
      setMessage({ text: successText, isError: false });
      return true;
    } catch (err) {
      setMessage({
        text: err instanceof ApiError ? err.message : "Something went wrong.",
        isError: true,
      });
      return false;
    } finally {
      setBusy(null);
    }
  };

  const handleSaveUrl = () => {
    const trimmed = urlInput.trim();
    if (!trimmed) {
      setMessage({ text: "URL cannot be empty.", isError: true });
      return;
    }
    runAction("url", () => setKioskUrl(pin, trimmed), "Kiosk URL updated.");
  };

  const handleChangePin = async () => {
    if (!/^\d{4}$/.test(newPin)) {
      setMessage({ text: "New PIN must be exactly 4 digits.", isError: true });
      return;
    }
    if (newPin !== newPinConfirm) {
      setMessage({ text: "New PINs do not match.", isError: true });
      return;
    }
    const ok = await runAction("pin", () => setKioskPin(pin, newPin), "PIN updated.");
    if (ok) {
      setNewPin("");
      setNewPinConfirm("");
    }
  };

  const handleOpen = () => runAction("open", () => openKiosk(pin), "Kiosk opened.");
  const handleClose = () => runAction("close", () => closeKiosk(pin), "Kiosk closed.");
  const handleReload = () => runAction("reload", () => reloadKiosk(pin), "Reload requested.");

  return (
    <div className="min-h-screen bg-gray-50 flex justify-center p-4 sm:p-6 font-sans">
      <div className="w-full max-w-md">
        <h1 className="text-xl font-semibold text-gray-800 mb-1">Kiosk Management</h1>
        <p className="text-sm text-gray-500 mb-4">
          Manage this kiosk over the local network. Bookmark this page — you
          only need to scan the QR code again if the kiosk's network IP
          changes.
        </p>

        {loadError && (
          <div className="bg-red-50 border border-red-300 text-red-700 rounded-md text-sm px-3 py-2 mb-4">
            {loadError}
          </div>
        )}

        {state && (
          <div className="bg-white rounded-2xl shadow-sm border border-gray-200 p-4 mb-4">
            <div className="text-sm font-medium text-gray-700 mb-2">Status</div>
            <dl className="text-sm text-gray-600 space-y-1">
              <div className="flex justify-between">
                <dt>Kiosk mode</dt>
                <dd className={state.enabled ? "text-success font-medium" : "text-gray-500"}>
                  {state.enabled ? "Enabled" : "Disabled"}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt>Active now</dt>
                <dd>{state.kioskActive ? "Yes" : "No"}</dd>
              </div>
              <div className="flex justify-between">
                <dt>PIN configured</dt>
                <dd>{state.hasPIN ? "Yes" : "No"}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="shrink-0">Current URL</dt>
                <dd className="truncate text-right" title={state.url}>
                  {state.url || "—"}
                </dd>
              </div>
            </dl>
          </div>
        )}

        {message && (
          <div
            className={`rounded-md text-sm px-3 py-2 mb-4 border ${
              message.isError
                ? "bg-red-50 border-red-300 text-red-700"
                : "bg-green-50 border-green-300 text-green-700"
            }`}
          >
            {message.text}
          </div>
        )}

        <div className="bg-white rounded-2xl shadow-sm border border-gray-200 p-4 mb-4">
          <label className="block text-sm font-medium text-gray-700 mb-1">PIN</label>
          <input
            type="password"
            inputMode="numeric"
            maxLength={4}
            placeholder="Required for every action below"
            className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-odoo-light focus:border-transparent"
            value={pin}
            onChange={(e) => setPin(e.target.value.replace(/\D/g, "").slice(0, 4))}
          />
          <p className="text-xs text-gray-400 mt-1">
            {state?.hasPIN
              ? "Enter the kiosk PIN to open, close, reload, or change settings."
              : "No PIN is set yet — set one below before enabling kiosk mode."}
          </p>
        </div>

        <div className="bg-white rounded-2xl shadow-sm border border-gray-200 p-4 mb-4">
          <label className="block text-sm font-medium text-gray-700 mb-1">Kiosk URL</label>
          <input
            type="url"
            placeholder="https://your-pos-url.example.com"
            className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-odoo-light focus:border-transparent mb-2"
            value={urlInput}
            onChange={(e) => setUrlInput(e.target.value)}
          />
          <button
            disabled={busy === "url"}
            onClick={handleSaveUrl}
            className="w-full border rounded-lg px-4 py-2 text-sm bg-odoo text-white hover:bg-odoo-dark disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
          >
            {busy === "url" ? "Saving…" : "Save URL"}
          </button>
        </div>

        <div className="bg-white rounded-2xl shadow-sm border border-gray-200 p-4 mb-4">
          <label className="block text-sm font-medium text-gray-700 mb-1">
            {state?.hasPIN ? "Change PIN" : "Set PIN"}
          </label>
          <div className="flex gap-2 mb-2">
            <input
              type="password"
              inputMode="numeric"
              maxLength={4}
              placeholder="New PIN"
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-odoo-light focus:border-transparent"
              value={newPin}
              onChange={(e) => setNewPin(e.target.value.replace(/\D/g, "").slice(0, 4))}
            />
            <input
              type="password"
              inputMode="numeric"
              maxLength={4}
              placeholder="Confirm"
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-odoo-light focus:border-transparent"
              value={newPinConfirm}
              onChange={(e) => setNewPinConfirm(e.target.value.replace(/\D/g, "").slice(0, 4))}
            />
          </div>
          <button
            disabled={busy === "pin"}
            onClick={handleChangePin}
            className="w-full border rounded-lg px-4 py-2 text-sm bg-odoo text-white hover:bg-odoo-dark disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
          >
            {busy === "pin" ? "Saving…" : state?.hasPIN ? "Change PIN" : "Set PIN"}
          </button>
        </div>

        <div className="bg-white rounded-2xl shadow-sm border border-gray-200 p-4">
          <div className="text-sm font-medium text-gray-700 mb-2">Kiosk controls</div>
          <div className="grid grid-cols-1 gap-2">
            <button
              disabled={busy === "open"}
              onClick={handleOpen}
              className="w-full border rounded-lg px-4 py-2 text-sm bg-success text-white hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
            >
              {busy === "open" ? "Opening…" : "Open kiosk"}
            </button>
            <button
              disabled={busy === "close"}
              onClick={handleClose}
              className="w-full border rounded-lg px-4 py-2 text-sm bg-danger text-white hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
            >
              {busy === "close" ? "Closing…" : "Close kiosk"}
            </button>
            <button
              disabled={busy === "reload"}
              onClick={handleReload}
              className="w-full border rounded-lg px-4 py-2 text-sm bg-gray-100 text-gray-700 hover:bg-gray-200 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
            >
              {busy === "reload" ? "Reloading…" : "Reload kiosk page"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
