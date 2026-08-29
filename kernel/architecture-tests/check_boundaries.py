#!/usr/bin/env python3
from pathlib import Path
import json,re,sys
ROOT=Path(__file__).resolve().parents[1];errors=[]
kernel_go=list((ROOT/'internal').rglob('*.go'))+list((ROOT/'sdk'/'go'/'protocol').rglob('*.go'))+list((ROOT/'cmd'/'kernel').rglob('*.go'))
# Kernel/protocol cannot depend on business plugins or business contract packages.
for p in kernel_go:
 t=p.read_text()
 if '/plugins/' in t or '/examples/' in t or '/sdk/go/contracts/' in t: errors.append(f'{p.relative_to(ROOT)} imports business implementation/contract')
# Plugins/examples cannot import kernel internals or other plugin implementation packages.
for base in [ROOT/'plugins',ROOT/'examples']:
 for p in base.rglob('*.go'):
  t=p.read_text()
  if 'agent-native-microkernel/internal/' in t: errors.append(f'{p.relative_to(ROOT)} imports kernel internal package')
  if re.search(r'agent-native-microkernel/(?:plugins|examples)/[^\"]+',t): errors.append(f'{p.relative_to(ROOT)} imports another plugin implementation')
# Manifest + contract consistency.
catalog=json.loads((ROOT/'contracts'/'catalog.json').read_text()); ids=set(); manifests=[]; exports={}; authority_storage={}
for p in sorted((ROOT/'plugins').glob('*.manifest.json')):
 d=json.loads(p.read_text());pid=d['plugin']['id'];manifests.append((p,d))
 if pid in ids: errors.append(f'duplicate plugin id {pid}')
 ids.add(pid)
 for section in ['exports']:
  for c in d.get(section,[]):
   k=f"{c.get('capability')}@{int(c.get('major',0))}"
   if not c.get('capability') or int(c.get('major',0))<=0: errors.append(f'{p.name}: invalid export {c}');continue
   if not c.get('contract'): errors.append(f'{p.name}: {k} missing contract id')
   elif k not in catalog: errors.append(f'{p.name}: {k} missing contract catalog entry')
   exports.setdefault(k,set()).add(c.get('contract'))
   if c.get('mode')=='stateful':
    if not c.get('service') or not c.get('authority'): errors.append(f'{p.name}: stateful {k} lacks service/authority')
    ns=d.get('runtime',{}).get('data_namespace')
    if not ns: errors.append(f'{p.name}: stateful {k} lacks runtime.data_namespace storage identity')
    else:
     ak=(c.get('service'),c.get('authority'))
     prev=authority_storage.get(ak)
     if prev and prev!=ns: errors.append(f'{p.name}: authority {ak[0]}/{ak[1]} storage conflict: {prev} vs {ns}')
     authority_storage.setdefault(ak,ns)
schema_meta={}
for k,rel in catalog.items():
 q=ROOT/'contracts'/rel
 if q.exists():
  try: schema_meta[k]=json.loads(q.read_text())
  except Exception: pass
for p,d in manifests:
 required=d.get('consumes',{}).get('required',[])
 optional=d.get('consumes',{}).get('optional',[])
 for c in required+optional:
  k=f"{c.get('capability')}@{int(c.get('major',0))}"
  if k not in exports and c in required: errors.append(f'{p.name}: missing provider for required {k}')
  if not c.get('contract'): errors.append(f'{p.name}: consumed {k} missing contract id')
  elif k not in catalog: errors.append(f'{p.name}: consumed {k} absent from catalog')
  elif k in exports and c.get('contract') not in exports[k]: errors.append(f'{p.name}: contract mismatch for {k}: consumer={c.get("contract")} providers={exports[k]}')
  elif schema_meta.get(k,{}).get('kind')=='event': errors.append(f'{p.name}: request/reply consume uses event contract {k}')
 for section in ('publishes','subscribes'):
  for c in d.get(section,[]):
   k=f"{c.get('capability')}@{int(c.get('major',0))}"
   if not c.get('contract'): errors.append(f'{p.name}: {section} {k} missing contract id')
   elif k not in catalog: errors.append(f'{p.name}: {section} {k} absent from catalog')
   elif schema_meta.get(k,{}).get('kind')!='event': errors.append(f'{p.name}: {section} {k} must use kind=event contract')
for k,rel in catalog.items():
 q=ROOT/'contracts'/rel
 if not q.exists(): errors.append(f'catalog {k} points to missing {rel}')
 else:
  try: d=json.loads(q.read_text())
  except Exception as e: errors.append(f'{rel}: invalid JSON {e}');continue
  if d.get('contract')!=k: errors.append(f'{rel}: contract identity {d.get("contract")} != {k}')
# Kernel domain vocabulary guard.
forbidden={'Task','Session','Agent','Workflow','Git','Knowledge','Spring','JPA','Weather','Learning','ReviewGate','WorkContext'}
pat=re.compile(r'^\s*(?:type|func)\s+(?:\([^)]*\)\s*)?([A-Z][A-Za-z0-9_]*)',re.M)
for p in kernel_go:
 for name in pat.findall(p.read_text()):
  if any(w.lower() in name.lower() for w in forbidden): errors.append(f'{p.relative_to(ROOT)} domain-specific public identifier: {name}')
if errors:
 print('ARCHITECTURE FITNESS: FAILED');[print(' -',e) for e in errors];sys.exit(1)
print('ARCHITECTURE FITNESS: PASSED')
print(f'checked {len(manifests)} plugin manifests, {len(catalog)} contract identities, stateful authority/storage declarations, cross-plugin imports and kernel vocabulary')
