#!/usr/bin/env python3
import json,os,signal,subprocess,tempfile,time,pathlib,sys
ROOT=pathlib.Path(__file__).resolve().parents[2]
BIN=ROOT/'bin'; PLUGINS=ROOT/'plugins'; POLICY=ROOT/'policy.json'

def wait_socket(p):
 for _ in range(300):
  if pathlib.Path(p).exists(): return
  time.sleep(.03)
 raise RuntimeError('socket not ready')

def start(root,sock,logpath):
 env=os.environ.copy();env['VIBE_DATA_ROOT']=str(root)
 log=open(logpath,'wb')
 p=subprocess.Popen([str(BIN/'vibe-kernel'),'-plugins',str(PLUGINS),'-policy',str(POLICY),'-socket',sock],cwd=ROOT,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 wait_socket(sock);return p,log

def call(sock,*args,identity='local-cli',token='m05-local-cli-token',ok=True):
 cp=subprocess.run([str(BIN/'vibe'),'-socket',sock,'-identity',identity,'-token',token,*args],cwd=ROOT,text=True,capture_output=True,timeout=10)
 if ok and cp.returncode: raise AssertionError(cp.stderr)
 if not ok and cp.returncode==0: raise AssertionError('expected failure')
 return cp

def payload(cp): return json.loads(cp.stdout)

def child_pid(kernel_pid,needle):
 ps=subprocess.check_output(["ps","-eo","pid=,ppid=,args="],text=True)
 for line in ps.splitlines():
  parts=line.strip().split(None,2)
  if len(parts)!=3 or int(parts[1])!=kernel_pid: continue
  exe=parts[2].split()[0]
  if exe.endswith('/'+needle): return int(parts[0])
 return None


with tempfile.TemporaryDirectory(prefix='vibe-m05-') as td:
 data=pathlib.Path(td)/'data';sock=str(pathlib.Path(td)/'kernel.sock');log=str(pathlib.Path(td)/'kernel.log')
 p,l=start(data,sock,log)
 try:
  # real external client + host authorization
  call(sock,'-cap','work.get','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"none"}',identity='evil',token='m05-local-cli-token',ok=False)
  # authenticated identity still needs an explicit capability grant
  call(sock,'-cap','work.create','-kind','command','-service','default-work-registry','-authority','workdb-main','-idempotency','readonly/deny','-payload','{"id":"DENIED","title":"must not write"}',identity='read-only-cli',token='m05-read-only-token',ok=False)
  # Event security is host policy, not manifest intent: only the granted subscriber receives it,
  # and TCB-derived caller/principal provenance must survive the real process path.
  ev=payload(call(sock,'-cap','event.probe.emit','-kind','command','-payload','{"value":"classified"}'))
  assert ev['published'] is True
  allowed_evt=data/'org.vibe.event.probe.subscriber.allowed'/'received.json'
  denied_evt=data/'org.vibe.event.probe.subscriber.denied'/'received.json'
  for _ in range(50):
   if allowed_evt.exists(): break
   time.sleep(.02)
  assert allowed_evt.exists(), 'authorized event subscriber did not receive event'
  assert not denied_evt.exists(), 'subscriber with manifest intent but no host grant received sensitive event'
  observed=json.loads(allowed_evt.read_text())
  assert observed['caller']=='org.vibe.event.probe.publisher', observed
  assert observed['principal']=='local-cli', observed
  assert observed['actor_chain']==['local-cli','org.vibe.event.probe.publisher'], observed
  # A raw/malicious plugin may forge Envelope.Caller/Principal, but TCB metadata must be rewritten.
  allowed_evt.unlink()
  forged=payload(call(sock,'-cap','event.probe.forge','-kind','command','-payload','{"value":"spoof-attempt"}'))
  assert forged['sent'] is True
  for _ in range(50):
   if allowed_evt.exists(): break
   time.sleep(.02)
  assert allowed_evt.exists(), 'forged event did not reach authorized subscriber'
  observed=json.loads(allowed_evt.read_text())
  assert observed['caller']=='org.vibe.event.probe.malicious', observed
  assert observed['principal']=='local-cli', observed
  assert observed['actor_chain']==['local-cli','org.vibe.event.probe.malicious'], observed
  assert not denied_evt.exists(), 'denied subscriber received forged sensitive event'
  # confused deputy: a low-privilege principal may invoke the composition, but
  # nested work.create must still be denied by principal ∩ plugin authority.
  esc=call(sock,'-cap','workflow.demo.run','-kind','command','-payload','{"work_id":"ESC2","title":"must-not-escalate","prompt":"x"}',identity='limited-user',token='m05-limited-user-token',ok=False)
  absent=call(sock,'-cap','work.get','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"ESC2"}',ok=False)
  assert 'NOT_FOUND' in absent.stderr, absent.stderr
  # Explicit delegated authority: a user may invoke the high-level workflow without
  # receiving direct work.create permission. The host-issued delegation scope is immutable.
  direct=call(sock,'-cap','work.create','-kind','command','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"DELEG-DIRECT","title":"must-deny"}',identity='workflow-user',token='m05-workflow-user-token',ok=False)
  delegated=payload(call(sock,'-cap','workflow.demo.run','-kind','command','-payload','{"work_id":"DELEG1","title":"delegated","prompt":"safe-delegation"}',identity='workflow-user',token='m05-workflow-user-token'))
  assert delegated['status']=='DONE', delegated
  # Contract kind is executable semantics: a query may not invoke a command contract.
  km=call(sock,'-cap','work.create','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"Q1","title":"created-via-query"}',ok=False)
  assert 'KIND_MISMATCH' in km.stderr, km.stderr
  qabsent=call(sock,'-cap','work.get','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"Q1"}',ok=False)
  assert 'NOT_FOUND' in qabsent.stderr, qabsent.stderr
  # Ordinary command cancellation: caller deadline must cancel provider work before side effects.
  marker='must-not-commit'
  cancel=call(sock,'-cap','cancel.probe','-kind','command','-timeout','100ms','-payload','{"delay_ms":700,"marker":"'+marker+'"}',ok=False)
  time.sleep(.9)
  assert not (data/'org.vibe.cancel.probe'/marker).exists(), 'timed-out command continued and committed after cancellation'
  # provider/business errors must survive routing unchanged (not INVOKE_ERROR / ROUTE_ERROR)
  missing=call(sock,'-cap','work.get','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"DOES-NOT-EXIST"}',ok=False)
  assert 'NOT_FOUND: work not found' in missing.stderr, missing.stderr
  assert 'INVOKE_ERROR' not in missing.stderr and 'ROUTE_ERROR' not in missing.stderr, missing.stderr
  # idempotency survives repeated command
  a=payload(call(sock,'-cap','work.create','-kind','command','-service','default-work-registry','-authority','workdb-main','-idempotency','create/T17','-payload','{"id":"T17","title":"qualification"}'))
  b=payload(call(sock,'-cap','work.create','-kind','command','-service','default-work-registry','-authority','workdb-main','-idempotency','create/T17','-payload','{"id":"T17","title":"qualification"}'))
  assert a['work']['version']==1 and b['idempotent_replay'] is True
  # Adversarial fencing: a writer may remain alive and continue after callers time out.
  # Three request-path failures open its circuit and promote the replica with a higher epoch;
  # stale in-flight writes from the old runtime must then fail at the storage fence.
  for i in range(3):
   stale=call(sock,'-cap','fence.probe.write','-kind','command','-service','fence-probe','-authority','fence-main','-timeout','120ms','-payload',json.dumps({'marker':f'stale-{i}','delay_ms':900}),ok=False)
  fresh=payload(call(sock,'-cap','fence.probe.write','-kind','command','-service','fence-probe','-authority','fence-main','-timeout','2s','-payload','{"marker":"fresh","delay_ms":0}'))
  assert fresh['committed'] is True and fresh['fencing_epoch'] >= 2, fresh
  time.sleep(1.15)
  fence_dir=data/'state-authority/fence-main'
  assert (fence_dir/'fresh').exists(), 'promoted writer did not commit'
  assert not any((fence_dir/f'stale-{i}').exists() for i in range(3)), 'stale writer bypassed fencing epoch after promotion'
  # Stateful authority has one active writer: provider_hint cannot route a command to a live replica.
  nonwriter=call(sock,'-cap','work.create','-kind','command','-provider','org.vibe.work.registry.replica','-service','default-work-registry','-authority','workdb-main','-idempotency','nonwriter/deny','-payload','{"id":"SPLIT1","title":"must-not-write"}',ok=False)
  splitabsent=call(sock,'-cap','work.get','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"SPLIT1"}',ok=False)
  assert 'NOT_FOUND' in splitabsent.stderr
  # Durable acknowledgement: force the provider's atomic temp write to fail; command must fail and state must remain absent.
  state_dir=data/'state-authority/workdb-main'; state_dir.mkdir(parents=True,exist_ok=True)
  poison=state_dir/'work.json.tmp'
  if poison.exists():
   if poison.is_dir(): poison.rmdir()
   else: poison.unlink()
  poison.mkdir()
  ioerr=call(sock,'-cap','work.create','-kind','command','-service','default-work-registry','-authority','workdb-main','-idempotency','io/fail','-payload','{"id":"IOFAIL","title":"must-not-ack"}',ok=False)
  poison.rmdir()
  ioabsent=call(sock,'-cap','work.get','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"IOFAIL"}',ok=False)
  assert 'NOT_FOUND' in ioabsent.stderr
  # external client stream: CLI is not a plugin, but consumes agent stream frames
  ext=subprocess.run([str(BIN/'vibe'),'-socket',sock,'-identity','local-cli','-token','m05-local-cli-token','-cap','agent.execute','-kind','command','-stream','-payload','{"prompt":"external-stream","steps":3,"delay_ms":10}'],cwd=ROOT,text=True,capture_output=True,timeout=8)
  assert ext.returncode==0, ext.stderr
  assert ext.stdout.count('stream.data')==3 and 'stream.close' in ext.stdout
  # External stream control path: the real CLI cancels after three frames over a second gateway connection.
  excancel=subprocess.run([str(BIN/'vibe'),'-socket',sock,'-identity','local-cli','-token','m05-local-cli-token','-cap','agent.execute','-kind','command','-stream','-cancel-after','3','-payload','{"prompt":"external-cancel","steps":100,"delay_ms":15}'],cwd=ROOT,text=True,capture_output=True,timeout=6)
  assert excancel.returncode==0, (excancel.stdout,excancel.stderr)
  assert excancel.stdout.count('stream.data')==3 and 'CANCELLED' in excancel.stderr, (excancel.stdout,excancel.stderr)
  # External consumer disconnect must cancel the provider-side stream instead of leaking work.
  disconnect_marker='client-disconnected'
  dis=subprocess.Popen([str(BIN/'vibe'),'-socket',sock,'-identity','local-cli','-token','m05-local-cli-token','-cap','agent.execute','-kind','command','-stream','-payload',json.dumps({'prompt':'disconnect','steps':200,'delay_ms':15,'cancel_marker':disconnect_marker})],cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  time.sleep(.16)
  dis.kill(); dis.communicate(timeout=2)
  marker_path=data/'org.vibe.agent.mock'/disconnect_marker
  for _ in range(60):
   if marker_path.exists(): break
   time.sleep(.02)
  assert marker_path.exists(), 'external stream consumer disconnect did not cancel provider work'
  # Provider crash during a live external stream must close the stream explicitly, not hang the client.
  crash=subprocess.Popen([str(BIN/'vibe'),'-socket',sock,'-identity','local-cli','-token','m05-local-cli-token','-cap','agent.execute','-kind','command','-stream','-payload','{"prompt":"crash-stream","steps":100,"delay_ms":30}'],cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  time.sleep(.15)
  mp=child_pid(p.pid,'mock-agent'); assert mp, 'mock-agent child not found for crash test'
  os.kill(mp,signal.SIGKILL)
  cout,cerr=crash.communicate(timeout=6)
  assert crash.returncode!=0 and 'PROVIDER_DISCONNECTED' in cerr, (crash.returncode,cout,cerr)
  # Required dependency health propagates: stream-probe remains alive, but its
  # exported capability is BLOCKED/unroutable while agent.execute has no healthy provider.
  time.sleep(.10)
  depblocked=call(sock,'-cap','stream.probe','-kind','command','-payload','{"prompt":"must-block","cancel_after":1}',ok=False)
  assert 'provider' in depblocked.stderr.lower() or 'unavailable' in depblocked.stderr.lower(), depblocked.stderr
  time.sleep(.70) # allow restart policy + dependency reconciliation to restore the provider
  # real stream + backpressure path + cancellation
  s=payload(call(sock,'-cap','stream.probe','-kind','command','-payload','{"prompt":"stream","cancel_after":3}'))
  assert s['chunks']==3 and s['cancelled'] is True and s['trace_id'] and s['correlation_id']
  # composition + nested trace propagation + durable journal
  w=payload(call(sock,'-cap','workflow.demo.run','-kind','command','-payload','{"work_id":"T18","title":"workflow","prompt":"implement"}'))
  assert w['status']=='DONE'
  j=payload(call(sock,'-cap','event.journal.replay','-service','default-event-journal','-authority','journal-main','-payload','{"after":0,"limit":20}'))
  assert len(j['records'])>=3
  tagged=[x for x in j['records'] if json.loads(json.dumps(x.get('payload',{}))).get('work_id')=='T18']
  assert len(tagged)>=2 and all(x['trace_id']==w['trace_id'] and x['correlation_id']==w['correlation_id'] for x in tagged), tagged
  agent_related=[x for x in j['records'] if x['type']=='agent.accepted' and x['trace_id']==w['trace_id']]
  assert len(agent_related)>=1 and all(x['correlation_id']==w['correlation_id'] for x in agent_related), agent_related
  assert all(x.get('caller') and x.get('principal') for x in j['records']), 'journal records must contain TCB-derived provenance'
  # Journal replay verifies its hash chain; tampering a persisted Source Asset must be detected.
  journal_path=data/'state-authority/journal-main/events.jsonl'
  original=journal_path.read_bytes(); tampered=original.replace(b'"source"',b'"sourcf"',1); assert tampered!=original
  journal_path.write_bytes(tampered)
  integ=call(sock,'-cap','event.journal.replay','-service','default-event-journal','-authority','journal-main','-payload','{"after":0,"limit":20}',ok=False)
  assert 'INTEGRITY_ERROR' in integ.stderr, integ.stderr
  journal_path.write_bytes(original)
  # plugin crash/restart retains owned state
  assert p.poll() is None, f'kernel exited early rc={p.returncode}'
  wp=child_pid(p.pid,'work-registry'); assert wp, 'primary work-registry child not found'
  os.kill(wp,signal.SIGKILL);time.sleep(.5)
  g=payload(call(sock,'-cap','work.get','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"T18"}'))
  assert g['work']['status']=='DONE'
 finally:
  os.kill(p.pid,signal.SIGTERM);p.wait(timeout=5);l.close()
 # kernel restart retains state and journal
 try: os.remove(sock)
 except FileNotFoundError: pass
 p,l=start(data,sock,str(pathlib.Path(td)/'kernel2.log'))
 try:
  g=payload(call(sock,'-cap','work.get','-service','default-work-registry','-authority','workdb-main','-payload','{"id":"T18"}')); assert g['work']['status']=='DONE'
  j2=payload(call(sock,'-cap','event.journal.replay','-service','default-event-journal','-authority','journal-main','-payload','{"after":0,"limit":20}')); assert len(j2['records'])>=3
 finally:
  os.kill(p.pid,signal.SIGTERM);p.wait(timeout=5);l.close()
# Abrupt kernel death with an in-flight side effect: managed plugin transport must die with the host,
# so the delayed command cannot commit after the kernel is SIGKILLed.
with tempfile.TemporaryDirectory(prefix='vibe-m05-kill-') as kd:
 data2=pathlib.Path(kd)/'data'; sock2=str(pathlib.Path(kd)/'kernel.sock'); lp=str(pathlib.Path(kd)/'kernel.log')
 kp,kl=start(data2,sock2,lp)
 client=subprocess.Popen([str(BIN/'vibe'),'-socket',sock2,'-identity','local-cli','-token','m05-local-cli-token','-cap','cancel.probe','-kind','command','-timeout','5s','-payload','{"delay_ms":1200,"marker":"after-kernel-death"}'],cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 time.sleep(.15); os.kill(kp.pid,signal.SIGKILL); kp.wait(timeout=3); kl.close()
 try: client.communicate(timeout=3)
 except subprocess.TimeoutExpired: client.kill(); client.communicate(); raise AssertionError('client hung after kernel SIGKILL')
 time.sleep(1.35)
 assert not (data2/'org.vibe.cancel.probe'/'after-kernel-death').exists(), 'plugin committed after kernel was SIGKILLed'

print('M0.5 ADVERSARIAL QUALIFICATION: PASSED')
print('validated: client authentication + host grants; confused-deputy prevention + explicit delegated authority; query/command kind enforcement; provider-error passthrough; ordinary request cancellation; external stream live-read/cancel/disconnect + provider-SIGKILL close; event publish/subscribe grants + host-rewritten actor provenance; stateful single-writer fencing against stale live writers; durable state acknowledgement + journal hash-chain tamper detection; trace/deadline propagation; dynamic required-dependency health; plugin/kernel restart including in-flight kernel SIGKILL')
