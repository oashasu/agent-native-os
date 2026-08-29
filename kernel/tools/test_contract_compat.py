#!/usr/bin/env python3
import copy,json,subprocess,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]; base=json.loads((ROOT/'contracts/work.create/v1/schema.json').read_text())
def run(mut,expect_ok,label):
 old=copy.deepcopy(base);new=copy.deepcopy(base);mut(new);new['version']='1.1.0'
 with tempfile.TemporaryDirectory() as td:
  a=Path(td)/'a.json';b=Path(td)/'b.json';a.write_text(json.dumps(old));b.write_text(json.dumps(new))
  cp=subprocess.run(['python',str(ROOT/'tools/contract_compat.py'),str(a),str(b)],capture_output=True,text=True)
  assert (cp.returncode==0)==expect_ok,(label,cp.stdout,cp.stderr)
run(lambda n:n['request']['properties'].update({'note':{'type':'string'}}),True,'optional request field')
run(lambda n:(n['request']['properties'].update({'must':{'type':'string'}}),n['request']['required'].append('must')),False,'new required request')
run(lambda n:(n['response']['properties'].update({'must_now_exist':{'type':'string'}}),n['response']['required'].append('must_now_exist')),False,'new required response')
run(lambda n:n['response']['properties']['work']['properties']['status'].update({'enum':['DONE']}),False,'nested enum')
run(lambda n:n['request'].update({'additionalProperties':True}),False,'additionalProperties')
run(lambda n:n.update({'kind':'query'}),False,'operation kind')
run(lambda n:n['request'].update({'$defs':{'x':{'type':'string'}}}),False,'opaque $defs change')
run(lambda n:n['request']['properties']['title'].update({'pattern':'^[A-Z]'}),False,'pattern constraint')
# New optional response field is safe for this contract because response admits unknown fields.
run(lambda n:n['response']['properties'].update({'note':{'type':'string'}}),True,'open response optional field')
# Nested required-field changes must be detected, not just top-level ones.
run(lambda n:(n['response']['properties']['work']['properties'].update({'owner':{'type':'string'}}),n['response']['properties']['work']['required'].append('owner')),False,'nested required response')
print('CONTRACT COMPAT TESTS: PASSED')
