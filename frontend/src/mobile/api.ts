// Thin fetch() wrapper for the /api/kiosk/* routes. No Wails bindings —
// this module (and everything under src/mobile/) must work standalone in a
// phone browser.

export type KioskState = {
  url: string;
  enabled: boolean;
  kioskActive: boolean;
  hasPIN: boolean;
};

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
  });

  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      // response wasn't JSON; keep the generic message
    }
    throw new ApiError(message, res.status);
  }

  return res.json() as Promise<T>;
}

export function getKioskState(): Promise<KioskState> {
  return request<KioskState>("/api/kiosk/state");
}

export function setKioskUrl(pin: string, url: string): Promise<KioskState> {
  return request<KioskState>("/api/kiosk/url", {
    method: "POST",
    body: JSON.stringify({ pin, url }),
  });
}

export function setKioskPin(pin: string, newPin: string): Promise<KioskState> {
  return request<KioskState>("/api/kiosk/pin", {
    method: "POST",
    body: JSON.stringify({ pin, newPin }),
  });
}

export function openKiosk(pin: string): Promise<KioskState> {
  return request<KioskState>("/api/kiosk/open", {
    method: "POST",
    body: JSON.stringify({ pin }),
  });
}

export function closeKiosk(pin: string): Promise<KioskState> {
  return request<KioskState>("/api/kiosk/close", {
    method: "POST",
    body: JSON.stringify({ pin }),
  });
}

export function reloadKiosk(pin: string): Promise<KioskState> {
  return request<KioskState>("/api/kiosk/reload", {
    method: "POST",
    body: JSON.stringify({ pin }),
  });
}
