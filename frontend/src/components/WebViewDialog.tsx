import { useContext, useEffect, useState } from "react";
import { WebViewContext } from "../contexts/WebViewContext";
import Dialog from "./Dialog";
import RemoteKioskSection from "./RemoteKioskSection";

export default function WebViewDialog() {
  const { data, actions } = useContext(WebViewContext);
  const cfg = data.config;

  const [url, setUrl] = useState(cfg?.url ?? "");
  const [pin, setPin] = useState("");
  const [pinConfirm, setPinConfirm] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [enablePending, setEnablePending] = useState(false);

  // Sync URL field when config loads
  useEffect(() => {
    if (cfg?.url) setUrl(cfg.url);
  }, [cfg?.url]);

  const saveSettings = async (): Promise<boolean> => {
    setLocalError(null);

    const trimmedUrl = url.trim();
    if (!trimmedUrl) {
      setLocalError("URL cannot be empty.");
      return false;
    }

    // Validate PIN only when user is typing a new one
    if (pin) {
      if (!/^\d{4}$/.test(pin)) {
        setLocalError("PIN must be exactly 4 digits.");
        return false;
      }
      if (pin !== pinConfirm) {
        setLocalError("PINs do not match.");
        return false;
      }
    }

    try {
      await actions.saveURL(trimmedUrl);
      if (pin) {
        await actions.savePIN(pin);
      }
      return true;
    } catch (err: any) {
      setLocalError(err?.toString() ?? "Failed to save settings.");
      return false;
    }
  };

  const handleToggleEnable = async () => {
    if (!cfg) return;
    setEnablePending(true);
    try {
      await actions.toggleEnabled(!cfg.enabled);
    } finally {
      setEnablePending(false);
    }
  };

  const cleanup = () => {
    setPin("");
    setPinConfirm("");
    setLocalError(null);
  };

  const canEnable = Boolean(cfg?.url && cfg?.hasPIN);

  return (
    <Dialog
      title="Kiosk / WebView Mode"
      validateText="Save"
      validateCallback={saveSettings}
      isValidateDisabled={url.trim() === ""}
      onClose={cleanup}
      openButton={
        <div
          className={`border-2 border-dashed rounded-lg px-4 py-3 cursor-pointer transition-colors ${
            cfg?.enabled
              ? "border-odoo/60 bg-odoo/5 text-odoo hover:bg-odoo/10"
              : "border-gray-300 bg-gray-50 text-gray-600 hover:border-gray-400 hover:bg-gray-100"
          }`}
        >
          {cfg?.enabled ? "🖥 Kiosk Mode ON" : "🖥 Kiosk Mode"}
        </div>
      }
    >
      <div className="flex flex-col gap-4 mb-4">
        {/* URL */}
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-1">
            URL
          </label>
          <input
            autoFocus
            type="url"
            placeholder="https://your-pos-url.example.com"
            className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-odoo-light focus:border-transparent"
            value={url}
            onChange={(e) => {
              setUrl(e.target.value);
              setLocalError(null);
            }}
          />
        </div>

        {/* PIN */}
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-1">
            {cfg?.hasPIN ? "Change PIN (leave blank to keep current)" : "Set PIN (4 digits)"}
          </label>
          <div className="flex gap-2">
            <input
              type="password"
              inputMode="numeric"
              maxLength={4}
              placeholder="••••"
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-odoo-light focus:border-transparent"
              value={pin}
              onChange={(e) => {
                setPin(e.target.value.replace(/\D/g, "").slice(0, 4));
                setLocalError(null);
              }}
            />
            <input
              type="password"
              inputMode="numeric"
              maxLength={4}
              placeholder="Confirm"
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-odoo-light focus:border-transparent"
              value={pinConfirm}
              onChange={(e) => {
                setPinConfirm(e.target.value.replace(/\D/g, "").slice(0, 4));
                setLocalError(null);
              }}
            />
          </div>
        </div>

        {/* Error */}
        {localError && (
          <div className="bg-red-50 border border-red-300 text-red-700 rounded-md text-sm px-3 py-2">
            {localError}
          </div>
        )}

        {/* Enable toggle */}
        <div
          className={`flex items-center justify-between p-3 rounded-lg border transition-colors ${
            cfg?.enabled
              ? "border-odoo/40 bg-odoo/5"
              : "border-gray-200 bg-gray-50"
          }`}
        >
          <div>
            <div className="text-sm font-medium text-gray-700">
              Kiosk mode
            </div>
            <div className="text-xs text-gray-400 mt-0.5">
              {canEnable
                ? "Opens URL fullscreen on launch"
                : "Save a URL and PIN first"}
            </div>
          </div>
          <button
            disabled={!canEnable || enablePending}
            onClick={handleToggleEnable}
            className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors cursor-pointer
              ${cfg?.enabled ? "bg-odoo" : "bg-gray-300"}
              disabled:opacity-40 disabled:cursor-not-allowed`}
          >
            <span
              className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
                cfg?.enabled ? "translate-x-6" : "translate-x-1"
              }`}
            />
          </button>
        </div>

        <p className="text-xs text-gray-400 -mt-2">
          Hidden unlock gesture: tap any screen edge 4× quickly, then enter your PIN.
        </p>

        <RemoteKioskSection />
      </div>
    </Dialog>
  );
}
