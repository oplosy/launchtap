/**
 * Whether the docs' side pieces (contents list, margin line) should be showing: hidden at the top of
 * the page, shown from the first scroll, hidden again once the end of the article reaches the
 * sticky contents list. The list would start scrolling away there, so everything fades a little
 * before that and dissolves in place, all at the same moment.
 */
export function docsSideShown(layout: ParentNode) {
  const article = layout.querySelector(".docs-article");
  const contents = layout.querySelector(".docs-contents");
  if (!article || !contents) return window.scrollY > 0;
  const ended =
    article.getBoundingClientRect().bottom <= contents.getBoundingClientRect().bottom + 96;
  return window.scrollY > 0 && !ended;
}
