#!/usr/bin/env python3
"""Conservative same-major JSON-Schema compatibility checker.

Usage: contract_compat.py OLD_SCHEMA NEW_SCHEMA

This checker intentionally prefers a false negative (requiring a major bump)
over a false claim of compatibility. It understands the object/array/scalar
subset used by the platform contract generator and treats changes to unsupported
validation keywords conservatively.
"""
import json,sys

ANNOTATION_KEYS={'title','description','default','examples','$comment','deprecated','readOnly','writeOnly'}
HANDLED_KEYS={
 'type','properties','required','additionalProperties','items',
 'oneOf','anyOf','allOf','not','enum','const','format','pattern','multipleOf',
 'minimum','exclusiveMinimum','maximum','exclusiveMaximum','minLength','maxLength','minItems','maxItems'
}

def canon(v): return json.dumps(v,sort_keys=True,separators=(',',':'))
def pathstr(path): return '.'.join(path) if path else '$'
def err(errors,path,msg): errors.append(f'{pathstr(path)}: {msg}')

def compare_unhandled(old,new,path,errors):
    # Keywords we do not model recursively must remain byte-semantically equal.
    # This covers $ref/$defs/if/then/else/dependentSchemas/contains/etc. without
    # ever incorrectly declaring an unknown schema evolution compatible.
    keys=(set(old)|set(new))-ANNOTATION_KEYS-HANDLED_KEYS
    for k in sorted(keys):
        if canon(old.get(k))!=canon(new.get(k)):
            err(errors,path,f'unsupported/opaque keyword {k} changed')

def compare_schema(old,new,path,mode,errors):
    if not isinstance(old,dict) or not isinstance(new,dict):
        if canon(old)!=canon(new): err(errors,path,'schema form changed')
        return
    compare_unhandled(old,new,path,errors)
    # Composition/constraints are conservative: semantic changes require a major bump.
    for k in ('oneOf','anyOf','allOf','not','enum','const','format','pattern','multipleOf'):
        if canon(old.get(k)) != canon(new.get(k)):
            err(errors,path,f'{k} changed')
    ot,nt=old.get('type'),new.get('type')
    if canon(ot)!=canon(nt): err(errors,path,f'type changed {ot!r} -> {nt!r}')
    for k in ('minimum','exclusiveMinimum','maximum','exclusiveMaximum','minLength','maxLength','minItems','maxItems'):
        if old.get(k)!=new.get(k): err(errors,path,f'{k} changed {old.get(k)!r} -> {new.get(k)!r}')
    if old.get('additionalProperties',True)!=new.get('additionalProperties',True):
        err(errors,path,'additionalProperties policy changed')
    if (ot=='array' or nt=='array' or 'items' in old or 'items' in new):
        compare_schema(old.get('items',{}),new.get('items',{}),path+['[]'],mode,errors)
    if ot=='object' or nt=='object' or 'properties' in old or 'properties' in new:
        op,np=old.get('properties',{}),new.get('properties',{})
        oldreq,newreq=set(old.get('required',[])),set(new.get('required',[]))
        if mode=='request':
            added=newreq-oldreq
            if added: err(errors,path,f'new required request fields {sorted(added)}')
            # Removing required input is a relaxation and is safe if the property remains.
        else:
            # Old consumers may rely on every previously required output and strict
            # generated clients can also break when new required output is introduced.
            if oldreq!=newreq: err(errors,path,f'response required set changed {sorted(oldreq)} -> {sorted(newreq)}')
        for name,osch in op.items():
            if name not in np: err(errors,path+[name],'field removed')
            else: compare_schema(osch,np[name],path+[name],mode,errors)
        for name in np.keys()-op.keys():
            # A new optional request field is acceptable for new providers; a new
            # response field is safe only if old consumers admitted unknown output.
            if mode=='response' and old.get('additionalProperties',True) is False:
                err(errors,path+[name],'new response field rejected by old additionalProperties=false')

def main():
    if len(sys.argv)!=3: print('usage: contract_compat.py OLD NEW',file=sys.stderr);return 2
    old=json.load(open(sys.argv[1]));new=json.load(open(sys.argv[2]));errors=[]
    if old.get('contract')!=new.get('contract'): errors.append('contract identity changed')
    if old.get('kind')!=new.get('kind'): errors.append(f'operation kind changed {old.get("kind")} -> {new.get("kind")}')
    try:
        omajor=int(str(old.get('version','')).split('.')[0]); nmajor=int(str(new.get('version','')).split('.')[0])
        if omajor!=nmajor: errors.append('major version changed (not a same-major comparison)')
    except Exception: errors.append('invalid semantic version')
    compare_schema(old.get('request',{}),new.get('request',{}),['request'],'request',errors)
    compare_schema(old.get('response',{}),new.get('response',{}),['response'],'response',errors)
    if errors:
        print('INCOMPATIBLE');[print(' -',e) for e in errors];return 1
    print('COMPATIBLE');return 0
if __name__=='__main__': sys.exit(main())
