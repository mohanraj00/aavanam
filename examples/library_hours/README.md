# Library example: one update through an activation to a gated head

This example moves one update from a source document to the head of a gated collection.
A fact producer admits two facts. An activation record names the approved content manifest.
The consumer resolves its typed references. The store then checks the activation before it moves the head.

1. The fact producer reads the synthetic source `hours.txt` and admits the opening hours of two branches.
2. The admitted-facts adapter writes the facts (ADP-FXT-1 to ADP-FXT-7). The application declares the branch and dates
   of each fact in an overlay (DSC-FLT-2), and makes a candidate snapshot of `sample-library-hours`.
3. A publish with no activation gives `activation_required` (PUB-GAT-3). The head does not move.
4. The consumer builds a content manifest from the snapshot members and the stored build lock. The record
   names the exact manifest bytes. The consumer checks and resolves the record, then writes the activation.
   A publish with this activation moves the head (PUB-ACT-3).
5. A filtered read for the north branch on 2026-02-10 gives one fact. The filter uses an exact `branch` match
   and a `contains` range match on `valid_from` and `valid_until` (DSC-FLT-1 to DSC-FLT-5, DAT-RNG-2).

The test [tests/test_library_example.py](../../tests/test_library_example.py) compares the output with this file.

## Activation record

The activation record comes from entelos v1.0.0. It is signed by the producer key.
This example does NOT check the signature, `record_ref`, `key_id` or the key set.
It decodes the record bytes and takes `record_kind` from the envelope of `first_signed.json` without a check.
The example prints one line before step 4 to show this.
A real consumer must check the signed record (SIGNED.md) before the consumer mapping, for example with
`entelos-verify --keys keyset.json first_signed.json`. It must take the record bytes and the record kind
only from a successful check. The seam test `tests/test_activation_seam.py` does this check against the pinned entelos.
The store does not evaluate the effective interval or verify an actor (PUB-ACT-5).
The application validator decides (PUB-RCP-2).

The file `first_signed.json` holds the signed activation record. The record bytes inside it
declare `candidate.ref` as the SHA-256 of the content manifest bytes.
This reference is `sha256:` and the SHA-256 of the exact UTF-8 manifest bytes in `manifest.json`.
It is not the store candidate identity.

The shared file `activation_mapping.py` supplies `resolve(record, record_kind, manifests, consumer)` to the example and test.
It first calls the shared `check_record` for record rules 1 to 8: record kind, JSON object, format,
field names, schema, candidate fields, subject and interval. The caller supplies the record kind. In this example, it comes from the envelope without a check.
The manifest has format `entelos.content/1`, the collection, the partition, the base manifest reference and
items in ascending `item_id` order. Each content member has its stored descriptor digest and byte size.
The item `inputs.lock` has the digest and byte size of the stored build lock. Stored descriptor text is canonical
JSON. Its SHA-256 equals its store reference (IDN-CAN-2), so this item names the exact lock bytes.

| Descriptor value | Typed record field and consumer mapping |
|---|---|
| candidate | `candidate.ref` selects the consumer store candidate |
| lock | `candidate.ref` selects the checked manifest; `inputs.lock` selects stored bytes by digest and size |
| base | `base` selects the published store snapshot; empty gives no base |
| approval | `decision_ref` selects a store receipt wrapper |
| report | `report_ref` selects a store receipt wrapper |
| partition | `partition`, checked against the manifest |
| effective_from, effective_until | `from`, `until` |
| request_key, producer, runner, approver | The record fields with the same names |

The consumer checks the manifest hash, format, schema, field names, item order and digest form. Its collection,
partition and base must equal the record fields. A lock must exist with the declared digest and size. A nonempty
base must select a snapshot in the consumer publication table and equal its current head. A repeat of an earlier
publication remains safe (PUB-HED-5). A failed check gives a refusal reason before any store call.

Receipt wrappers contain only the typed record references. Receipt content stays in the producer.
The descriptor profile and kind are local constants. This first update removes no member (PUB-ACT-2).
The collection and partition names agree by fixture design. This is no general mapping of the two head models.
The fact applicability comes from application overlays, not from the empty record `applicability`.
The guard test requires each descriptor value to come from `resolve`.

## Files

- `library_hours.py`: the example.
- `activation_mapping.py`: the shared consumer mapping. It uses only the standard library.
- `hours.txt`: the synthetic source document. It names no real library, place or person.
- `first_signed.json`: the signed activation record from entelos v1.0.0.
- `first_record.json`: the activation record bytes from entelos v1.0.0.
- `manifest.json`: the content manifest of the update.
- `keyset.json`: the public key set (`entelos.keyset/1`) of the signer.
- `requirements.txt`: the fact producer at version 0.15.0, pinned with its hash. A text source needs no extra dependency.

## Applicability is declared metadata

The facts of the adapter have no branch and no dates. The application owns the applicability policy, so it declares the values in the table `APPLICABILITY` of `library_hours.py`. The example writes them as overlays about each fact, in an overlay set of the candidate snapshot. The store compares the values, and does not interpret them (DSC-FLT-4). The lock of the activation covers this overlay set, so the activation confirms the values (DSC-FLT-2).

The filter reads `list_items` items. Each fact is one data resource, so the filter selects one fact. A filtered `export` selects knowledge units (BUN-FLT-1). The adapter writes one knowledge unit for each export, so a filtered export cannot select one fact of this source.

## Run the example from a clean checkout

You need Python 3.14 or later.

1. Do steps 1 to 4 of [Run an example from a clean checkout](../README.md#run-an-example-from-a-clean-checkout). Then the environment `../aavanam-user` has the store wheel.

2. Install the pinned fact producer in the same environment:

   ```bash
   ../aavanam-user/bin/python -m pip install --require-hashes --only-binary :all: --no-deps -r examples/library_hours/requirements.txt
   ```

3. Run the example:

   ```bash
   ../aavanam-user/bin/python -I examples/library_hours/library_hours.py
   ```

Output of `library_hours.py`:

```text
producer north_winter admitted
producer south_summer admitted
adapter wrote 2 facts to sample-library-hours
publish with no activation: incompatible ['activation_required'] head None
signature of the activation record: not checked in this example
publish with the activation: ok published head is the candidate snapshot True
filter branch north date 2026-02-10 gives 1 of 2 facts
fact north_winter '09:00 to 17:00'
```
