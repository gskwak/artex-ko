// Mock toggle. Public variables injected at build time need the NEXT_PUBLIC_ prefix to be available in the browser.
// Set NEXT_PUBLIC_MOCK=1 on Vercel to use mock data throughout the site without a backend.
export const MOCK = process.env.NEXT_PUBLIC_MOCK === "1";
