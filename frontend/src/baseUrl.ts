// The ingress redirects /daq to /daq/ before serving the application.
// Vite's relative base keeps assets and every RPC on that same mount path.
export function daqBaseUrl(pageUrl = window.location.href): string {
  return new URL('.', pageUrl).href
}
