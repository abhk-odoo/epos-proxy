import { useEffect, useState } from "react";
import { GetRemoteKioskInfo } from "../../wailsjs/go/main/App";
import { main } from "../../wailsjs/go/models";

/**
 * "Remote Kiosk Management" section shown inside the Kiosk/WebView dialog.
 *
 * Shows a QR code encoding exactly http://<lan-ip>:<port>/kiosk — no
 * pairing code, token, or PIN. Scanning it once opens the mobile
 * kiosk-management page; from there the user can bookmark that URL and use
 * it directly next time, only re-scanning if the machine's LAN IP changes.
 */
export default function RemoteKioskSection() {
  const [info, setInfo] = useState<main.RemoteKioskInfo | null>(null);
  const [selectedIp, setSelectedIp] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = async (ip: string) => {
    setLoading(true);
    setError(null);
    try {
      const result = await GetRemoteKioskInfo(ip);
      setInfo(result);
      if (!ip && result.lanIPs.length > 0) {
        setSelectedIp(result.lanIPs[0]);
      }
    } catch (err: any) {
      setError(err?.toString() ?? "Failed to load remote kiosk info.");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleIpChange = (ip: string) => {
    setSelectedIp(ip);
    load(ip);
  };

  return (
    <div className="border-t border-gray-200 pt-4 mt-2">
      <div className="text-sm font-medium text-gray-700 mb-1">
        Remote Kiosk Management
      </div>
      <p className="text-xs text-gray-400 mb-3">
        Scan this QR code from a phone on the same network to open the mobile
        kiosk-management page. Bookmark it — you only need to scan again if
        this machine's network IP changes.
      </p>

      {loading && (
        <div className="text-sm text-gray-400 text-center py-4">Loading…</div>
      )}

      {error && (
        <div className="bg-red-50 border border-red-300 text-red-700 rounded-md text-sm px-3 py-2 mb-3">
          {error}
        </div>
      )}

      {!loading && !error && info && (
        <>
          {info.lanIPs.length === 0 ? (
            <div className="text-sm text-gray-500 text-center py-2">
              No local network connection detected.
            </div>
          ) : (
            <div className="flex flex-col items-center gap-3">
              {info.lanIPs.length > 1 && (
                <select
                  value={selectedIp}
                  onChange={(e) => handleIpChange(e.target.value)}
                  className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-odoo-light focus:border-transparent"
                >
                  {info.lanIPs.map((ip) => (
                    <option key={ip} value={ip}>
                      {ip}
                    </option>
                  ))}
                </select>
              )}

              {info.qrDataURI && (
                <img
                  src={info.qrDataURI}
                  alt="Kiosk management QR code"
                  className="w-40 h-40 rounded-lg border border-gray-200"
                />
              )}

              <div className="w-full">
                <label className="block text-xs font-medium text-gray-500 mb-1">
                  Mobile kiosk URL
                </label>
                <input
                  readOnly
                  value={info.mobileURL}
                  onFocus={(e) => e.currentTarget.select()}
                  className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm bg-gray-50 text-gray-600 select-all"
                />
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
