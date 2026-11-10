# Announcement draft: Aavanam 0.1.0

Status: draft. Nothing in this file is posted. The owner approves each outreach step and each channel.

## Short form

> Aavanam 0.1.0 is out. It's a small versioned knowledge store: put a document, a table and the evidence between them into one immutable snapshot, and your chat agent, your scripts and your code generator all read the same bytes. Each run can lock the exact inputs it used. Spec first, 400 conformance vectors, and a small reference implementation you can read end to end. https://github.com/mohanraj00/aavanam

## Long form

**Aavanam 0.1.0: every consumer reads the same snapshot**

I kept seeing the same failure. One body of knowledge, a handbook, say, gets copied into three shapes. The chat agent gets text chunks. The scripts get a CSV. The code generator gets a schema someone pasted into a repo. Six months later the three copies disagree, and nobody can tell which version produced which answer.

Aavanam is my attempt at the boring fix. Producers put documents, typed tables, rule files and evidence into a store. A snapshot pins one exact set of them, and a changed byte is a new object, so a snapshot never moves. Every consumer opens the same snapshot. When a script runs, it can write a lock: the exact objects it read, by digest.

A few things it deliberately does not do:

- It doesn't run your rules. Rule files and emitter templates are opaque bytes to the store.
- It doesn't guess. Search never fills in a missing value, and "unknown" never turns into a default.
- It doesn't claim more than it knows. A lock is complete over the references it declares, and says so.

0.1.0 is a first marker, not a product. The 16 contract documents are drafts. There are 400 conformance vectors, and the reference implementation passes all of them; a null implementation fails 399 of them, which is the point. The spec does not fix a language or a storage engine. The 0.1.0 reference store happens to be the Python standard library over SQLite, so you can read every line of it in an afternoon.

The README has a short example that stores a short manual and a table, reads them from an agent and a script, and locks a run. It runs in the test suite, so it stays true.

What I'd like to hear about: where your own agents, scripts and generators drift apart today, and whether a snapshot and a lock would have caught it.

https://github.com/mohanraj00/aavanam

## Checklist before a post

- [ ] The owner approves the text.
- [ ] The owner approves each channel.
- [ ] The owner uploads `docs/brand/social-preview.png` as the social preview of the repository.
- [ ] The links and the vector counts agree with the release.
