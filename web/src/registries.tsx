import { ExternalLink, HardDrive } from "lucide-react";
import { Link } from "react-router-dom";
import { text } from "./i18n";
import { Icon } from "./ui";

// An image name says where it comes from: a registry host first ("ghcr.io/…"),
// Docker Hub without one, MyInfra itself for the images a person brought.

export const localPrefix = "myinfra.local/";

const known: Record<string, string> = {
  "docker.io": "Docker Hub",
  "ghcr.io": "GitHub Container Registry",
  "quay.io": "Quay.io",
  "registry.gitlab.com": "GitLab Container Registry",
  "lscr.io": "LinuxServer.io",
  "codeberg.org": "Codeberg",
  "gcr.io": "Google Container Registry",
  "public.ecr.aws": "Amazon ECR Public",
  "mcr.microsoft.com": "Microsoft Artifact Registry",
};

export type ImageSource = {
  // The registry host, docker.io for a bare name, "" for an image MyInfra keeps.
  host: string;
  label: string;
  // The image without its version, as the registry names it.
  repository: string;
  tag: string;
  href?: string;
  local: boolean;
};

export function imageSource(image: string): ImageSource {
  const slash = image.lastIndexOf("/");
  const colon = image.lastIndexOf(":");
  const tagged = colon > slash;
  const name = tagged ? image.slice(0, colon) : image;
  const tag = tagged ? image.slice(colon + 1) : "latest";
  if (image.startsWith(localPrefix)) {
    return { host: "", label: text("registry.local"), repository: name.slice(localPrefix.length), tag, local: true };
  }
  const parts = name.split("/");
  const first = parts[0] ?? "";
  const hasHost = parts.length > 1 && (first.includes(".") || first.includes(":") || first === "localhost");
  const host = hasHost ? first : "docker.io";
  const repository = hasHost ? parts.slice(1).join("/") : name;
  const label = known[host] ?? (host.endsWith(".pkg.dev") ? "Google Artifact Registry" : host);
  return { host, label, repository, tag, href: registryHref(name), local: false };
}

export function registryHref(name: string) {
  const parts = name.split("/");
  const host = parts[0]?.includes(".") ? parts[0] : "";
  if (host === "ghcr.io") {
    const owner = parts[1];
    const pkg = parts.slice(2).join("/");
    return `https://github.com/orgs/${owner}/packages/container/package/${pkg}`;
  }
  if (host === "quay.io") return `https://quay.io/repository/${parts.slice(1).join("/")}`;
  if (host === "lscr.io") return `https://docs.linuxserver.io/images/docker-${parts[parts.length - 1]}/`;
  if (host === "codeberg.org") return `https://codeberg.org/${parts[1]}/-/packages/container/${parts.slice(2).join("/")}`;
  if (host === "public.ecr.aws") return `https://gallery.ecr.aws/${parts.slice(1).join("/")}`;
  if (host) return `https://${name}`;
  if (parts.length === 1) return `https://hub.docker.com/_/${name}`;
  return `https://hub.docker.com/r/${name}`;
}

// ImageOrigin shows where an image is pulled from: the registry and the image,
// linked to its page, or the imported images of MyInfra.
export function ImageOrigin({ image, compact = false }: { image: string; compact?: boolean }) {
  const source = imageSource(image);
  const where = source.local ? (
    <Link to="/images" className="inline-flex items-center gap-1 underline">
      <Icon icon={HardDrive} className="size-3.5" />
      {source.label}
    </Link>
  ) : (
    <span>{source.label}</span>
  );
  if (compact) return <span className="text-muted">{where}</span>;
  return (
    <span className="inline-flex min-w-0 flex-wrap items-baseline gap-x-2 text-muted">
      <span>{text("registry.from")}</span>
      {where}
      {source.href ? (
        <a href={source.href} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-1 break-all font-mono text-[13px] underline">
          {source.repository}:{source.tag}
          <Icon icon={ExternalLink} className="size-3.5 shrink-0" />
        </a>
      ) : (
        <span className="break-all font-mono text-[13px]">
          {source.repository}:{source.tag}
        </span>
      )}
    </span>
  );
}
