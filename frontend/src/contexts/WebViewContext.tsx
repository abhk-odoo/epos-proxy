import { createContext, useCallback, useEffect, useState } from "react";
import {
  GetWebViewConfig,
  SetWebViewEnabled,
  SetWebViewPIN,
  SetWebViewURL,
  SetWindowFullscreen,
  ValidateWebViewPIN,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export type WebViewConfig = {
  url: string;
  enabled: boolean;
  hasPIN: boolean;
};

type WebViewContextType = {
  data: {
    config: WebViewConfig | null;
    isKioskActive: boolean;
    reloadNonce: number;
  };
  actions: {
    saveURL: (url: string) => Promise<void>;
    savePIN: (pin: string) => Promise<void>;
    toggleEnabled: (v: boolean) => Promise<void>;
    validatePIN: (pin: string) => Promise<boolean>;
    exitKiosk: () => Promise<void>;
    enterKiosk: () => Promise<void>;
  };
};

export const WebViewContext = createContext({} as WebViewContextType);

interface WebViewContextWrapperProps {
  children: React.ReactNode;
}

export const WebViewContextWrapper = ({
  children,
}: WebViewContextWrapperProps) => {
  const [config, setConfig] = useState<WebViewConfig | null>(null);
  const [isKioskActive, setIsKioskActive] = useState(false);
  const [reloadNonce, setReloadNonce] = useState(0);

  const refresh = useCallback(async () => {
    try {
      const cfg = await GetWebViewConfig();
      setConfig(cfg);
      // Keep the kiosk overlay in sync with the actual enabled state —
      // this path is also hit when the mobile UI closes the kiosk
      // remotely, so it must be able to turn isKioskActive off, not just on.
      if (cfg.enabled && cfg.url) {
        setIsKioskActive(true);
        await SetWindowFullscreen(true);
      } else {
        setIsKioskActive(false);
        await SetWindowFullscreen(false);
      }
    } catch (err) {
      console.error("Failed to fetch WebView config:", err);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // The mobile /kiosk UI drives kiosk state via HTTP handlers that go
  // through the same App methods, then emit these events so the desktop
  // window picks up the change without polling.
  useEffect(() => {
    const offStateChanged = EventsOn("kiosk:state-changed", () => {
      refresh();
    });
    const offReload = EventsOn("kiosk:reload", () => {
      setReloadNonce((n) => n + 1);
    });
    return () => {
      offStateChanged();
      offReload();
    };
  }, [refresh]);

  const saveURL = async (url: string) => {
    await SetWebViewURL(url);
    await refresh();
  };

  const savePIN = async (pin: string) => {
    await SetWebViewPIN(pin);
    await refresh();
  };

  const toggleEnabled = async (v: boolean) => {
    await SetWebViewEnabled(v);
    await refresh();
  };

  const validatePIN = async (pin: string): Promise<boolean> => {
    return ValidateWebViewPIN(pin);
  };

  const enterKiosk = async () => {
    setIsKioskActive(true);
    await SetWindowFullscreen(true);
  };

  const exitKiosk = async () => {
    setIsKioskActive(false);
    await SetWindowFullscreen(false);
  };

  return (
    <WebViewContext.Provider
      value={{
        data: { config, isKioskActive, reloadNonce },
        actions: {
          saveURL,
          savePIN,
          toggleEnabled,
          validatePIN,
          enterKiosk,
          exitKiosk,
        },
      }}
    >
      {children}
    </WebViewContext.Provider>
  );
};
