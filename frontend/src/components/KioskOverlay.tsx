import { useContext, useRef, useState } from "react";
import { WebViewContext } from "../contexts/WebViewContext";
import PINModal from "./PINModal";

/** Radius (px) of the circular tap zone at each corner. */
const CORNER_RADIUS = 80;
/** Number of corner taps needed to open the PIN dialog. */
const REQUIRED_TAPS = 4;
/** Milliseconds before the tap counter resets to zero. */
const RESET_MS = 1000;

/**
 * Full-screen iframe overlay that locks the UI into kiosk mode.
 *
 * The iframe absorbs all pointer events — they cannot bubble to the parent
 * window. Four transparent circular zones are layered above the iframe at
 * each corner. Tapping any corner REQUIRED_TAPS times within RESET_MS ms
 * opens the PIN dialog.
 */
export default function KioskOverlay() {
  const { data, actions } = useContext(WebViewContext);
  const [pinVisible, setPinVisible] = useState(false);

  const tapCount = useRef(0);
  const resetTimer = useRef<number | null>(null);

  const handleCornerTap = () => {
    if (pinVisible) return;

    tapCount.current += 1;

    // Restart the decay timer on every valid tap
    if (resetTimer.current !== null) clearTimeout(resetTimer.current);
    resetTimer.current = window.setTimeout(() => {
      tapCount.current = 0;
    }, RESET_MS);

    if (tapCount.current >= REQUIRED_TAPS) {
      tapCount.current = 0;
      if (resetTimer.current !== null) {
        clearTimeout(resetTimer.current);
        resetTimer.current = null;
      }
      setPinVisible(true);
    }
  };

  const handlePINSuccess = async () => {
    setPinVisible(false);
    await actions.toggleEnabled(false);
  };

  const handlePINDismiss = () => {
    setPinVisible(false);
  };

  if (!data.isKioskActive || !data.config?.url) {
    return null;
  }

  // Base style for all four corner zones
  const cornerBase: React.CSSProperties = {
    position: "fixed",
    zIndex: 9995,          // above iframe (9990), below PIN modal (9999)
    width: CORNER_RADIUS,
    height: CORNER_RADIUS,
    borderRadius: "50%",   // circular hit area
    opacity: 0,            // invisible — purely a tap target
    cursor: "default",
  };

  return (
    <>
      {/* Top-right */}
      <div style={{ ...cornerBase, top: 0, right: 0 }} onPointerDown={handleCornerTap} />

      {/* ── Full-screen kiosk iframe ── */}
      <div
        className="fixed inset-0 bg-black"
        style={{ zIndex: 9990, pointerEvents: pinVisible ? "none" : "auto" }}
      >
        <iframe
          key={data.reloadNonce}
          src={data.config.url}
          title="Kiosk"
          className="w-full h-full border-0"
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        />
      </div>

      {/* ── PIN modal (above everything) ── */}
      {pinVisible && (
        <PINModal onSuccess={handlePINSuccess} onDismiss={handlePINDismiss} />
      )}
    </>
  );
}
