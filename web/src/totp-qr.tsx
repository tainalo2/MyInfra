import { useEffect, useState } from "react";
import QRCode from "qrcode";
import { text } from "./i18n";
import { totpProvisioningUri } from "./totp";

export function TotpQr({ secret, account, uri: uriProp }: { secret: string; account: string; uri?: string }) {
  const [src, setSrc] = useState<string | null>(null);

  useEffect(() => {
    let cancel = false;
    const uri = uriProp?.trim() || totpProvisioningUri(secret, account);
    QRCode.toDataURL(uri, { width: 200, margin: 2, errorCorrectionLevel: "M" })
      .then((url) => {
        if (!cancel) setSrc(url);
      })
      .catch(() => {
        if (!cancel) setSrc(null);
      });
    return () => {
      cancel = true;
    };
  }, [secret, account, uriProp]);

  if (!src) return null;

  return (
    <figure className="mt-4 flex flex-col items-start gap-2">
      <img src={src} width={200} height={200} alt={text("account.totpQr")} className="rounded-md border border-line bg-white p-2" />
      <figcaption className="text-[13px] text-muted">{text("account.totpQr")}</figcaption>
    </figure>
  );
}
