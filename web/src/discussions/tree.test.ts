import { describe, expect, it } from 'vitest'
import type { CommentView } from '~/api/gen/types.gen'
import { tree } from './tree'

const c = (ref: string, parentRef = ''): CommentView =>
  ({ ref, parentRef, apId: ref, bodyHtml: '', gone: false, published: '', score: 0 }) as CommentView

describe('tree', () => {
  it('nests replies and lifts orphans to the top level', () => {
    const roots = tree([c('a'), c('b', 'a'), c('c', 'b'), c('d', 'missing'), c('e')])
    expect(roots.map((n) => n.ref)).toEqual(['a', 'd', 'e'])
    expect(roots[0]?.replies[0]?.ref).toBe('b')
    expect(roots[0]?.replies[0]?.replies[0]?.ref).toBe('c')
  })
})
