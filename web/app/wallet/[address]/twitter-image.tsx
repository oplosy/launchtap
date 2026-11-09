import Image from "./opengraph-image";

// Route segment config must be declared literally in each file; it cannot be re-exported.
export const alt = "Wallet on Launchpad";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const revalidate = 300;

// An empty list makes this an ISR route, like opengraph-image.
export async function generateStaticParams() {
  return [];
}

export default Image;
