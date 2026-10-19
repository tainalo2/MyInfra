export function ProviderLogo({ id }: { id: string }) {
  switch (id) {
    case "cloudflare":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <path
            fill="#F38020"
            d="M11.7 7.05a2.55 2.55 0 0 0-4.85-.72A2.15 2.15 0 0 0 3.4 8.2a2 2 0 0 0 .15 4h7.55a2.15 2.15 0 0 0 .6-4.15Z"
          />
        </svg>
      );
    case "ovh":
      return (
        <svg width="24" height="16" viewBox="0 0 24 16" aria-hidden>
          <rect width="24" height="16" rx="3" fill="#123F9E" />
          <rect x="2.1" y="3.3" width="5.4" height="9.4" rx="2.5" fill="none" stroke="#fff" strokeWidth="1.35" />
          <path d="M9.3 3.6 11.4 12.4 13.5 3.6" fill="none" stroke="#fff" strokeWidth="1.35" strokeLinejoin="round" strokeLinecap="round" />
          <path d="M15.4 3.6v8.8M19.8 3.6v8.8M15.4 8h4.4" fill="none" stroke="#fff" strokeWidth="1.35" strokeLinecap="round" />
        </svg>
      );
    case "hetzner":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#D50C2D" />
          <path d="M4.2 3.6v8.8M11.8 3.6v8.8M4.2 8h7.6" fill="none" stroke="#fff" strokeWidth="1.7" strokeLinecap="round" />
        </svg>
      );
    case "scaleway":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#520DC2" />
          <path fill="#fff" d="M4 11.4 8 3.8l4 7.6H9.6L8 8.2 6.4 11.4H4Z" />
        </svg>
      );
    case "github":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#24292F" />
          <path
            fill="#fff"
            d="M8 3.2a4.8 4.8 0 0 0-1.5 9.35c.24.04.32-.1.32-.23v-.8c-1.32.29-1.6-.56-1.6-.56-.22-.56-.54-.7-.54-.7-.44-.3.03-.3.03-.3.48.04.74.5.74.5.43.74 1.13.52 1.4.4.04-.31.17-.52.3-.64-1.05-.12-2.16-.53-2.16-2.34 0-.52.18-.94.48-1.27-.05-.12-.21-.6.05-1.26 0 0 .4-.13 1.3.49a4.4 4.4 0 0 1 2.36 0c.9-.62 1.3-.49 1.3-.49.26.66.1 1.14.05 1.26.3.33.48.75.48 1.27 0 1.82-1.11 2.22-2.17 2.34.18.15.33.45.33.92v1.36c0 .13.08.28.33.23A4.8 4.8 0 0 0 8 3.2Z"
          />
        </svg>
      );
    case "digitalocean":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#0080FF" />
          <circle cx="8" cy="8" r="3.2" fill="#fff" />
        </svg>
      );
    case "infomaniak":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#009245" />
          <rect x="7.1" y="3.1" width="1.8" height="1.8" rx="0.4" fill="#fff" />
          <rect x="7.1" y="6" width="1.8" height="6.7" rx="0.4" fill="#fff" />
        </svg>
      );
    case "upcloud":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#0E1C36" />
          <path fill="#7CDE3A" d="M4.2 11.2V4.8h2.1c1.7 0 2.7.8 2.7 2.1 0 1.3-1 2.1-2.7 2.1H5.7v2.2H4.2Zm1.5-3.3h.5c.8 0 1.2-.4 1.2-1s-.4-1-1.2-1h-.5v2Z" />
        </svg>
      );
    case "vultr":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#007BFC" />
          <path fill="#fff" d="M4.2 3.8h7.6L8 12.2 4.2 3.8Z" />
        </svg>
      );
    case "akamai":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#0099D8" />
          <path fill="#fff" d="M8 3.2 12.6 12h-1.8L8 6.6 5.2 12H3.4L8 3.2Z" />
        </svg>
      );
    case "exoscale":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#DA291C" />
          <path fill="#fff" d="M4 4h7.6v1.7H5.8v1.5h5.1v1.6H5.8v1.5h5.8V12H4V4Z" />
        </svg>
      );
    case "gcore":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#FF4C00" />
          <path fill="none" stroke="#fff" strokeWidth="1.7" strokeLinecap="round" d="M11.4 5.6A3.7 3.7 0 1 0 11.7 9H8.3" />
        </svg>
      );
    case "kamatera":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#0F7BBF" />
          <path fill="none" stroke="#fff" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" d="M5 3.8v8.4M11 3.8 5.6 8l5.4 4.2" />
        </svg>
      );
    case "openstack":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#ED1944" />
          <circle cx="8" cy="8" r="3.1" fill="none" stroke="#fff" strokeWidth="1.4" />
        </svg>
      );
    case "proxmox":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#E57000" />
          <path fill="#fff" d="M4 11.4V4.6h1.5l2.5 4.2 2.5-4.2H12v6.8h-1.5V7.4L8.4 11h-.8L5.5 7.4v4H4Z" />
        </svg>
      );
    case "ionos":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect width="16" height="16" rx="3" fill="#003D8F" />
          <path fill="#fff" d="M4 4h1.6v8H4V4Zm3 0h1.6v8H7V4Zm3 0h2v8h-2V4Z" />
        </svg>
      );
    case "gitlab":
      return (
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <path fill="#E24329" d="M8 13.6 2.2 8.4 3.6 4.2l1.5 4.6h5.8l1.5-4.6 1.4 4.2L8 13.6Z" />
          <path fill="#FC6D26" d="M8 13.6 5.1 8.8h5.8L8 13.6Z" />
          <path fill="#FCA326" d="M5.1 8.8 3.6 4.2 2.2 8.4 5.1 8.8Zm5.8 0 1.5-4.6 1.4 4.2-2.9.4Z" />
        </svg>
      );
    default:
      return null;
  }
}
