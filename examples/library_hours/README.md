# Library example: one update from groundgate through an activation to a gated head

This example moves one update from a source document to the head of a gated collection. It uses three projects: groundgate admits the facts, an entelos activation record approves the update, and aavanam moves the head only with that record. It does the steps of issue #83:

1. groundgate reads the synthetic source `hours.txt` and admits two facts: the opening hours of the north branch in winter and of the south branch in summer.
2. The admitted-facts adapter writes the facts to the store (ADP-FXT-1 to ADP-FXT-7). The application declares the branch and the dates of each fact in an overlay (DSC-FLT-2), and makes the candidate snapshot of the gated collection `sample-library-hours`.
3. A publish with no activation is refused with the reason `activation_required` (PUB-GAT-3). The head does not move.
4. The activation record approves the update. The build lock covers each changed member of the snapshot (PUB-ACT-3 check 5). The publish with the activation moves the head.
5. A filtered read of the head for the north branch on 2026-02-10 gives one fact. The filter uses an exact match on `branch` and the range match `contains` on `valid_from` and `valid_until` (DSC-FLT-1 to DSC-FLT-5, DAT-RNG-2).

The test [tests/test_library_example.py](../../tests/test_library_example.py) runs the example and compares its output with the output in this file.

## Stand-in activation record

The activation record is a stand-in. It uses the proposed record table of `ACTIVATION.md` at commit `4d9d66e`.
No entelos release writes this record yet. Thus `requirements.txt` pins no release of the activation producer.

The file `activation_record_stand_in.json` uses the same layout as `tests/activation_record_stand_in.json`.
The seam test applies the consumer rules to both files, after it binds the store values. The record has no signature.
The store does not verify a signature or evaluate the effective interval (PUB-ACT-5). The application validator decides
(PUB-RCP-2). The mapping table is in the docstring of `tests/test_activation_seam.py`.

The example derives a build lock from the changed snapshot members. It writes store receipts with `decision_ref` and
`report_ref` as opaque payloads. These receipts contain references, not the receipt content of the producer. The candidate
binding is a test value, not proof that native identities are equal. The record has separate `collection` and `partition`
fields. This fixture gives both fields the store collection name. These choices prove no general producer adapter.
The applicability of the facts comes from the application overlays below, not from the empty record `applicability`.

When a release writes the activation record, a later change must do these steps:

1. Pin that release in `requirements.txt`.
2. Replace the stand-in with a record from that release.
3. Update `activation_of` and the rule checks if the released layout needs a change.

## Files

- `library_hours.py`: the example.
- `hours.txt`: the synthetic source document. It names no real library, place or person.
- `activation_record_stand_in.json`: the stand-in activation record.
- `requirements.txt`: groundgate 0.15.0, pinned with its hash. The example reads a text source, so groundgate needs no extra and no dependency.

## Applicability is declared metadata

The facts of the adapter have no branch and no dates. The application owns the applicability policy, so it declares the values in the table `APPLICABILITY` of `library_hours.py`. The example writes them as overlays about each fact, in an overlay set of the candidate snapshot. The store compares the values, and does not interpret them (DSC-FLT-4). The lock of the activation covers this overlay set, so the activation confirms the values (DSC-FLT-2).

The filter reads `list_items` items. Each fact is one data resource, so the filter selects one fact. A filtered `export` selects knowledge units (BUN-FLT-1). The adapter writes one knowledge unit for each export, so a filtered export cannot select one fact of this source.

## Run the example from a clean checkout

You need Python 3.14 or later.

1. Do steps 1 to 4 of [Run an example from a clean checkout](../README.md#run-an-example-from-a-clean-checkout). Then the environment `../aavanam-user` has the aavanam wheel.

2. Install the pinned groundgate in the same environment:

   ```bash
   ../aavanam-user/bin/python -m pip install --require-hashes --only-binary :all: --no-deps -r examples/library_hours/requirements.txt
   ```

3. Run the example:

   ```bash
   ../aavanam-user/bin/python -I examples/library_hours/library_hours.py
   ```

Output of `library_hours.py`:

```text
groundgate north_winter admitted
groundgate south_summer admitted
adapter wrote 2 facts to sample-library-hours
publish with no activation: incompatible ['activation_required'] head None
publish with the activation: ok published head is the candidate snapshot True
filter branch north date 2026-02-10 gives 1 of 2 facts
fact north_winter '09:00 to 17:00'
```
