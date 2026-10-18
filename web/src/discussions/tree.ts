import type { CommentView } from '~/api/gen/types.gen'

export type Node = CommentView & { replies: Node[] }

// tree nests comments under their parents. The API sends them oldest first,
// so a parent always comes before its replies. A reply whose parent is not in
// the list (held, or past the page limit) goes to the top level.
export function tree(comments: CommentView[]): Node[] {
  const byRef = new Map<string, Node>()
  const roots: Node[] = []
  for (const c of comments) {
    const node: Node = { ...c, replies: [] }
    byRef.set(c.ref, node)
    const parent = c.parentRef ? byRef.get(c.parentRef) : undefined
    if (parent) parent.replies.push(node)
    else roots.push(node)
  }
  return roots
}
