# objects/ — nouns

One card per type/interface, or per tightly-coupled small group of them that
an editor asks about as a single thing. Clustered by layer — see
`../CONTEXT.md`'s "How the clusters divide".

## Reading order

1. `_index.md` — one line per noun, says which cluster/card and whether it's
   `verified` or `stub`.
2. The cluster card itself.
3. The card's `See` link, which lands on real source — never on another card.

## Writing a card

Copy `../_templates/object.md`. Required: a citation for every claim in
`Shape`, and a first-order `Hits` / `Does not hit` in "If you change this".
`status: verified` needs a date and branch/commit alongside the citation.
