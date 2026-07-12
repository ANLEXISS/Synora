import { Camera, CameraOff, Square } from "lucide-react";
import { useEffect, useRef, useState } from "react";

type BarcodeResult = { rawValue?: string };
type BarcodeDetectorLike = {
  detect: (source: HTMLVideoElement) => Promise<BarcodeResult[]>;
};
type BarcodeDetectorConstructor = new (options?: { formats: string[] }) => BarcodeDetectorLike;

type QRCodeScannerProps = {
  onPayload: (payload: string) => void;
};

export function QRCodeScanner({ onPayload }: QRCodeScannerProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const frameRef = useRef<number | null>(null);
  const detectorRef = useRef<BarcodeDetectorLike | null>(null);
  const [running, setRunning] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  function stop() {
    if (frameRef.current !== null) {
      cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    }
    streamRef.current?.getTracks().forEach((track) => track.stop());
    streamRef.current = null;
    if (videoRef.current) videoRef.current.srcObject = null;
    setRunning(false);
  }

  async function start() {
    setMessage(null);
    const detectorConstructor = (window as Window & { BarcodeDetector?: BarcodeDetectorConstructor }).BarcodeDetector;
    if (!detectorConstructor) {
      setMessage("Scan QR non supporté dans ce navigateur, collez le code manuellement.");
      return;
    }
    if (!navigator.mediaDevices?.getUserMedia) {
      setMessage("La caméra de ce navigateur n’est pas disponible. Collez le code manuellement.");
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" } });
      streamRef.current = stream;
      detectorRef.current = new detectorConstructor({ formats: ["qr_code"] });
      if (!videoRef.current) return;
      videoRef.current.srcObject = stream;
      await videoRef.current.play();
      setRunning(true);

      const scan = async () => {
        if (!videoRef.current || !detectorRef.current || !streamRef.current) return;
        try {
          const results = await detectorRef.current.detect(videoRef.current);
          const value = results.find((result) => typeof result.rawValue === "string")?.rawValue;
          if (value) {
            onPayload(value);
            stop();
            return;
          }
        } catch {
          setMessage("Impossible de lire ce QR code. Essayez la saisie manuelle.");
          stop();
          return;
        }
        frameRef.current = requestAnimationFrame(() => void scan());
      };
      frameRef.current = requestAnimationFrame(() => void scan());
    } catch {
      stop();
      setMessage("Autorisation caméra refusée ou caméra indisponible. Collez le code manuellement.");
    }
  }

  useEffect(() => stop, []);

  return (
    <div className="qr-scanner">
      <div className="qr-preview">
        <video ref={videoRef} muted playsInline aria-label="Aperçu du scan QR" />
        {!running && <div className="qr-preview-placeholder"><Camera size={28} /><span>Aperçu caméra</span></div>}
      </div>
      <div className="qr-scanner-actions">
        {running ? (
          <button type="button" className="secondary-button" onClick={stop}><Square size={15} /> Arrêter</button>
        ) : (
          <button type="button" className="secondary-button" onClick={() => void start()}><Camera size={15} /> Démarrer le scan</button>
        )}
        <span className="qr-scanner-hint"><CameraOff size={14} /> Aucun secret n’est affiché après validation.</span>
      </div>
      {message && <p className="wizard-error">{message}</p>}
    </div>
  );
}
