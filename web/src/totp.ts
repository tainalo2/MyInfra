const issuer = "MyInfra";

/** URI scanné par Google Authenticator, FreeOTP, etc. (aligné sur auth.ProvisioningURI). */
export function totpProvisioningUri(secret: string, account: string): string {
  const name = account.trim() || issuer;
  const label = encodeURIComponent(`${issuer}:${name}`);
  const params = new URLSearchParams();
  params.set("secret", secret.replace(/\s/g, ""));
  params.set("issuer", issuer);
  params.set("algorithm", "SHA1");
  params.set("digits", "6");
  params.set("period", "30");
  return `otpauth://totp/${label}?${params.toString()}`;
}
