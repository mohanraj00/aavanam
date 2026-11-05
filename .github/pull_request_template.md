## Issue

Closes #

## Change

<!-- Describe the behavior that changed. Cite the clause IDs. -->

## Checks

- [ ] `python3 -B tools/check_project.py`
- [ ] `python3 -B tools/check_spec.py` and `python3 -B -m unittest discover -s conformance`
- [ ] `python3 -B -m unittest discover -s tests` and both runners (code changes only). The null implementation fails
- [ ] Each new refusal vector has a positive pair
- [ ] Code review: <harness and version, link to the comment> (a contributor without a harness writes "maintainer")
- [ ] Security review: <link to the comment, "maintainer", or "not needed" and the reason>
- [ ] Each finding is fixed, or has a reason in this PR

## Limits

<!-- State what you did not test and what is not supported. -->
