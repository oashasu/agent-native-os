#!/usr/bin/env python3
import json,sys
from pathlib import Path
from jsonschema import Draft202012Validator
ROOT=Path(__file__).resolve().parents[1]
catalog=json.loads((ROOT/'contracts/catalog.json').read_text());errors=[]
for ident,rel in catalog.items():
 p=ROOT/'contracts'/rel
 try:d=json.loads(p.read_text())
 except Exception as e: errors.append(f'{ident}: invalid JSON {e}');continue
 if d.get('contract')!=ident: errors.append(f'{ident}: identity mismatch')
 if not d.get('version','').startswith(str(ident.rsplit('@',1)[1])+'.'): errors.append(f'{ident}: version major mismatch')
 for side in ('request','response'):
  try: Draft202012Validator.check_schema(d[side])
  except Exception as e: errors.append(f'{ident} {side}: invalid JSON Schema: {e}')
 if d.get('compatibility')!='backward-within-major': errors.append(f'{ident}: compatibility policy missing')
if errors:
 print('CONTRACT CHECK: FAILED');[print(' -',x) for x in errors];sys.exit(1)
print(f'CONTRACT CHECK: PASSED ({len(catalog)} contracts, Draft 2020-12 schemas)')
