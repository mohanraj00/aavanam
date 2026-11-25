#!/usr/bin/env python3
"""Offline shared-harness layout, local-link and regular-source inventory check."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import sys

ROOT=Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--write-checksums',action='store_true');a=p.parse_args()
required=['CONSTITUTION.md','AGENTS.md','CLAUDE.md','GEMINI.md','README.md','CONTRIBUTING.md','SECURITY.md','LICENSE','SPEC.md',
    'docs/STATUS.md','docs/REVIEW.md','.agents/skills/spec-workflow/SKILL.md','.github/CODEOWNERS','.github/pull_request_template.md',
    '.github/workflows/ci.yml','.github/workflows/gates.yml','.github/workflows/gates-rerun.yml','.github/gates.json','.github/workflows/claude-review.yml',
    '.github/ISSUE_TEMPLATE/bug.yml','.github/ISSUE_TEMPLATE/spec.yml','.github/ISSUE_TEMPLATE/docs.yml','.github/ISSUE_TEMPLATE/config.yml']
errors=[];inventory={};links=0
for path in sorted(ROOT.rglob('*')):
    relative=path.relative_to(ROOT)
    if relative.parts[0]=='.git':continue
    name=relative.as_posix()
    if path.is_symlink():
        if name!='.claude/skills' or path.readlink().as_posix()!='../.agents/skills' or path.resolve()!=(ROOT/'.agents/skills').resolve():
            errors.append('unexpected or invalid symlink: '+name)
        continue
    if not path.is_file():continue
    if set(relative.parts)&{'__pycache__','.venv','node_modules'}:errors.append('generated source-side directory: '+name)
    if name!='checksums.json':inventory[name]=hashlib.sha256(path.read_bytes()).hexdigest()
    if path.suffix=='.md':
        for target in re.findall(r'\]\(([^)]+)\)',path.read_text()):
            if target.startswith(('https:','http:','#','mailto:')):continue
            links+=1
            if not (path.parent/target.split('#')[0]).exists():errors.append('broken local link: '+name+' -> '+target)
for name in required:
    if not (ROOT/name).is_file():errors.append('missing: '+name)
alias=ROOT/'.claude/skills'
if not alias.is_symlink():errors.append('missing shared skill link')
claude=(ROOT/'CLAUDE.md').read_text() if (ROOT/'CLAUDE.md').exists() else ''
if '@CONSTITUTION.md' not in claude or '@AGENTS.md' not in claude:errors.append('Claude entry point does not import shared rules')
gemini=(ROOT/'GEMINI.md').read_text() if (ROOT/'GEMINI.md').exists() else ''
if '@CONSTITUTION.md' not in gemini or '@AGENTS.md' not in gemini:errors.append('GEMINI.md does not import shared rules')
# Classification regressions: each path must keep its CI and security-review class under .github/gates.json.
# A path runs the project jobs if it is a code, security or level 1 path. The gates action owns the shared categories.
sys.path.insert(0,str(ROOT/'tools'))
from fnmatch import fnmatch
# A short copy of the shared security categories of the gates action (gates/gate.py of mohanraj00/.github at the gate pin).
AGENT=['AGENTS.md','CLAUDE.md','GEMINI.md','SKILL.md','.agents/*','.claude/*','.codex/*','.gemini/*']
MANIFESTS=['pyproject.toml','uv.lock','poetry.lock','Pipfile','Pipfile.lock','requirements*.txt','setup.py','setup.cfg']
SHARED=['.github/*','CONSTITUTION.md','docs/STANDARD.md','docs/REVIEW.md','SECURITY.md']+[y for x in AGENT+MANIFESTS for y in (x,'*/'+x)]
def security(rules,paths):
    hit=lambda x,keys:any(fnmatch(x,y) for y in keys)
    return [x for x in paths if hit(x,SHARED+rules.get('boundaries',[]))
        or (hit(x,rules.get('security_sources',[])) and not hit(x,rules.get('not_security',[])))]
rules=json.loads((ROOT/'.github/gates.json').read_text())
for path,code,sec in [('README.md',False,False),('docs/STATUS.md',False,False),('SPEC.md',True,False),('docs/contracts/13-publication.md',True,False),
        ('src/aavanam/store.py',True,True),('conformance/runner.py',True,True),('conformance/store_adapter.py',True,True),
        ('conformance/test_runner.py',True,False),('conformance/vectors/kinds/put-source-ok.json',True,False),('fixtures/README.md',True,False),
        ('tests/test_gates.py',True,False),('tools/check_spec.py',True,True),('tools/claude_review.py',True,True),('.github/workflows/ci.yml',True,True),
        ('.github/gates.json',True,True),('.codex/config.toml',True,True),('GEMINI.md',True,True),('docs/AGENTS.md',True,True),('docs/REVIEW.md',True,True),
        ('src/aavanam/.claude/settings.json',True,True),('tests/AGENTS.md',True,True),('tests/CLAUDE.md',True,True),('tests/.claude/settings.json',True,True)]:
    got_sec=bool(security(rules,[path]))
    got_code=got_sec or any(fnmatch(path,x) for x in rules['code']+rules['level1'])
    if (got_code,got_sec)!=(code,sec):errors.append('classification changed for %s: code=%s security=%s'%(path,got_code,got_sec))
# The gates action, the ci-ok classify step and the gate regression job use one full SHA of mohanraj00/.github.
pins=set()
for name in ['gates.yml','ci.yml']:
    text=(ROOT/'.github/workflows'/name).read_text()
    pins|=set(re.findall(r'mohanraj00/\.github/gates@(\S+)',text))|set(re.findall(r'repository: mohanraj00/\.github\n\s+ref: (\S+)',text))
if len(pins)!=1 or not re.fullmatch(r'[0-9a-f]{40}',next(iter(pins),'')):errors.append('the workflows must pin mohanraj00/.github to one full SHA: %s'%sorted(pins))
if 'test_gate.py' not in (ROOT/'.github/workflows/ci.yml').read_text():errors.append('ci.yml must run the gate regression tests of the pinned gates action')
# The gate checks the permission of the requester. Only the review job has the secret.
from claude_workflow import workflow_errors
errors+=['claude-review.yml '+x for x in workflow_errors((ROOT/'.github/workflows/claude-review.yml').read_text())]
# The shared gates/cross_review.py owns the agy fallback and its tests.
sys.path.insert(0,str(ROOT/'tools'));import counts
errors+=counts.problems(ROOT)  # the stated counts (issue #70)
# Each implementation lists the vectors that pass on it. A list holds each vector ID once, and the lists of one implementation hold all IDs.
# The IDs come from the vector files. The backend on memory has two lists, so the check adds them together (issue #108).
vector_ids={x.relative_to(ROOT/'conformance/vectors').with_suffix('').as_posix() for x in (ROOT/'conformance/vectors').rglob('*.json')}
# conformance/pending-go.txt names the vectors that a Go implementation does not pass yet, with the issue of the work.
# A vector must be in the lists of an implementation or in this file. A line with a `/**` at its end names a folder.
pending_lines=[x.strip() for x in (ROOT/'conformance/pending-go.txt').read_text(encoding='utf-8').splitlines()]
pending_lines=[x for x in pending_lines if x and not x.startswith('#')]
pending={v for v in vector_ids for x in pending_lines if v==x or (x.endswith('/**') and v.startswith(x[:-2]))}
unused=[x for x in pending_lines if not any(v==x or (x.endswith('/**') and v.startswith(x[:-2])) for v in vector_ids)]
if unused:errors.append('conformance/pending-go.txt: these lines name no vector: %s'%sorted(unused))
all_listed=None
for target,lists in [('backend on PostgreSQL',['backend/conformance/passing.txt']),
        ('backend on memory',['backend/conformance/passing-mem-core.txt','backend/conformance/passing-mem-read.txt']),
        ('device store',['reference/conformance/passing.txt'])]:
    listed=[]
    for name in lists:
        try:lines=[x.strip() for x in (ROOT/name).read_text(encoding='utf-8').splitlines()]
        except (OSError,UnicodeDecodeError) as e:
            errors.append('cannot read the vector list %s: %s'%(name,e));continue
        listed+=[x for x in lines if x and not x.startswith('#')]
    twice=sorted({x for x in listed if listed.count(x)>1})
    if twice:errors.append('%s: a vector ID is in the lists more than once: %s'%(target,twice))
    if set(listed)-vector_ids:errors.append('%s: the lists have IDs that are not vectors: %s'%(target,sorted(set(listed)-vector_ids)))
    if vector_ids-pending-set(listed):errors.append('%s: the lists do not have these vectors: %s'%(target,sorted(vector_ids-pending-set(listed))))
    all_listed=set(listed) if all_listed is None else all_listed&set(listed)
if pending&(all_listed or set()):errors.append('conformance/pending-go.txt: each Go implementation lists these vectors, so remove them: %s'%sorted(pending&all_listed))
checksum=ROOT/'checksums.json'
if a.write_checksums:
    if errors:raise SystemExit(json.dumps({'passed':False,'errors':errors}))
    checksum.write_text(json.dumps(inventory,indent=2,sort_keys=True)+'\n')
elif not checksum.exists() or json.loads(checksum.read_text())!=inventory:
    errors.append('source checksum inventory differs; review intended edits, then regenerate')
result={'passed':not errors,'regular_source_files':len(inventory),'local_links_checked':links,
    'shared_skill_link':alias.is_symlink() and alias.resolve()==(ROOT/'.agents/skills').resolve(),
    'checksums_updated':a.write_checksums,'errors':errors,'scope':'Source/layout integrity only; no model, runtime, platform-compatibility or production result.'}
print(json.dumps(result,indent=2));raise SystemExit(bool(errors))
