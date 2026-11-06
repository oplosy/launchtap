import Image from "./opengraph-image";

// Segment config must be declared literally in each file; it cannot be re-exported.
export const alt = "Token on Launchpad";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const revalidate = 300;

export default Image;
