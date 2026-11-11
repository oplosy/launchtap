import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PageHeader, Section } from "./layout";

describe("PageHeader", () => {
  it("renders exactly one h1 with the title and the summary", () => {
    const html = renderToStaticMarkup(
      createElement(PageHeader, { title: "Explore", summary: "Every launch." }),
    );
    expect(html.match(/<h1/g)).toHaveLength(1);
    expect(html).toContain(">Explore</h1>");
    expect(html).toContain("Every launch.");
  });
  it("renders the title as text, never as markup", () => {
    const html = renderToStaticMarkup(createElement(PageHeader, { title: "<b>x</b>" }));
    expect(html).toContain("&lt;b&gt;x&lt;/b&gt;");
  });
});

describe("Section", () => {
  it("labels itself by its heading and links View all", () => {
    const html = renderToStaticMarkup(
      createElement(
        Section,
        {
          id: "trending",
          title: "Trending",
          viewAllHref: "/explore",
        },
        "rows",
      ),
    );
    expect(html).toMatch(/<section[^>]*aria-labelledby="trending-title"/);
    expect(html).toMatch(/<h2[^>]*id="trending-title"[^>]*>Trending<\/h2>/);
    expect(html).toMatch(/<a[^>]*href="\/explore"[^>]*>View all/);
    expect(html).toContain("rows");
  });
  it("omits the link without viewAllHref", () => {
    const html = renderToStaticMarkup(createElement(Section, { id: "x", title: "X" }, "rows"));
    expect(html).not.toContain("View all");
  });
});
