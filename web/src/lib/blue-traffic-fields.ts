import type { BlueEvent } from "@/lib/blue-api";

function asRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function atPath(value: unknown, path: string): unknown {
  const record = asRecord(value);
  if (!record) return undefined;
  // Zeek uses literal dotted keys such as "id.orig_h"; other sources use nested objects.
  if (Object.hasOwn(record, path)) return record[path];
  let current: unknown = record;
  for (const part of path.split(".")) {
    const nested = asRecord(current);
    if (!nested) return undefined;
    current = nested[part];
  }
  return current;
}

function scalar(value: unknown): string {
  if (typeof value === "string") return value.trim();
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  return "";
}

function first(raw: unknown, paths: readonly string[]): string {
  // File imports preserve the whole normalized record. Prefer its inner telemetry
  // so envelope fields (for example an asset's host) cannot mask network fields.
  const inner = asRecord(asRecord(raw)?.raw);
  for (const candidate of inner ? [inner, raw] : [raw]) {
    for (const path of paths) {
      const value = scalar(atPath(candidate, path));
      if (value) return value;
    }
  }
  return "";
}

function endpoint(address: string, port: string): string {
  if (!address) return port;
  if (!port) return address;
  return `${address.includes(":") && !address.startsWith("[") ? `[${address}]` : address}:${port}`;
}

/** Extract common network fields from an imported event without altering its preserved raw record. */
export function trafficFields(event: BlueEvent) {
  const raw = event.raw;
  const sourceIP = first(raw, [
    "src_ip",
    "source_ip",
    "src.ip",
    "source.ip",
    "source.address",
    "id.orig_h",
    "orig_h",
    "client_ip",
    "remote_addr",
  ]);
  const sourcePort = first(raw, ["src_port", "source_port", "src.port", "source.port", "id.orig_p", "orig_p"]);
  const destinationIP = first(raw, [
    "dest_ip",
    "dst_ip",
    "destination_ip",
    "dest.ip",
    "dst.ip",
    "destination.ip",
    "destination.address",
    "id.resp_h",
    "resp_h",
    "server_ip",
  ]);
  const destinationPort = first(raw, [
    "dest_port",
    "dst_port",
    "destination_port",
    "dest.port",
    "dst.port",
    "destination.port",
    "id.resp_p",
    "resp_p",
  ]);
  const transportProtocol = first(raw, ["proto", "protocol", "network.transport", "transport"]);
  const applicationProtocol = first(raw, ["app_proto", "service", "network.protocol"]);
  const protocol =
    transportProtocol && applicationProtocol && transportProtocol.toLowerCase() !== applicationProtocol.toLowerCase()
      ? `${transportProtocol.toUpperCase()} / ${applicationProtocol.toUpperCase()}`
      : (transportProtocol || applicationProtocol).toUpperCase();
  const method = first(raw, ["http.http_method", "http.method", "http_method", "method"]);
  let host = first(raw, [
    "http.hostname",
    "http.host",
    "host",
    "hostname",
    "server_name",
    "tls.sni",
    "dns.rrname",
    "query",
  ]);
  const url = first(raw, ["http.url", "http.uri", "url", "uri", "path"]);
  if (!host && /^https?:\/\//i.test(url)) {
    try {
      host = new URL(url).hostname;
    } catch {
      // Preserve the original URL when an imported record is malformed.
    }
  }
  const status = first(raw, ["http.status", "http.status_code", "status_code", "response.status_code", "status"]);
  const action = first(raw, ["alert.action", "event.action", "action", "verdict"]);

  return {
    sourceIP,
    sourcePort,
    destinationIP,
    destinationPort,
    sourceEndpoint: endpoint(sourceIP, sourcePort),
    destinationEndpoint: endpoint(destinationIP, destinationPort),
    protocol,
    method,
    host,
    url,
    status,
    action,
  };
}
