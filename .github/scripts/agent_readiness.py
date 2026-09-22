#!/usr/bin/env python3
from __future__ import annotations
import base64, json, os, re, sys, urllib.error, urllib.request

REQUIRED = ["Objective","Dependencies","Collision Boundary","Contracts","Implementation Requirements","Test Strategy","Acceptance Criteria","Definition of Done","Handoff"]
ACTIVE_STATES = {"READY","IN_PROGRESS","REVIEW","INTEGRATION"}
BLOCKING_STATES = {"BLOCKED","REJECTED","CANCELLED"}

def fail(message):
    print("::error::" + message)
    sys.exit(1)

def api(path):
    req = urllib.request.Request(
        "https://api.github.com" + path,
        headers={"Accept":"application/vnd.github+json","Authorization":"Bearer "+os.environ["GITHUB_TOKEN"],"X-GitHub-Api-Version":"2022-11-28","User-Agent":"axiom-agent-readiness"})
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        fail("GitHub API %s for %s: %s" % (e.code, path, e.read().decode("utf-8","replace")[:300]))

def sections(body):
    return {m.group(1).strip() for line in body.splitlines() if (m := re.match(r"^#{2,4}\s+(.+?)\s*$", line))}

def labels(issue):
    return {x.get("name","") for x in issue.get("labels",[])}

def state_label(issue):
    for label in labels(issue):
        if label.startswith("agent:"):
            return label.split(":",1)[1].upper()
    return None

def refs(text):
    return sorted({int(m.group(1)) for m in re.finditer(r"(?<![\w-])#(\d+)\b", text or "")})

def norm(p):
    p=p.strip().replace("\\","/").lstrip("./")
    while "//" in p: p=p.replace("//","/")
    return p.rstrip("/")

def owned(path, roots):
    path=norm(path)
    return any(path == norm(r) or path.startswith(norm(r)+"/") for r in roots if norm(r))

def parse_owned(body):
    out=[]; active=False
    for line in body.splitlines():
        if re.match(r"^\s*(?:OWNED PATHS|Owned Paths|OWNED)\s*:?\s*$",line,re.I):
            active=True; continue
        if active and re.match(r"^\s*(?:READ/CONSUME|READ|FORBIDDEN|FORBIDDEN PATHS|Dependencies|##)\b",line,re.I):
            active=False
        if active:
            m=re.match(r"^\s*[-*]\s+(.+?)\s*$",line)
            if m:
                value=m.group(1).strip()
                if value and not value.startswith("#"): out.append(value)
    return [norm(x) for x in out if norm(x)]

def work_map(repo, number):
    try:
        data=api("/repos/%s/contents/docs/architecture/agent-work-map.md"%repo)
        content=base64.b64decode(data["content"]).decode("utf-8")
    except Exception:
        return []
    marker=re.compile(r"^\s*[-*]?\s*#?%d\b"%number,re.M)
    matches=list(marker.finditer(content))
    if not matches: return []
    start=matches[0].start()
    nxt=re.search(r"^\s*[-*]?\s*#?\d+\b",content[start+1:],re.M)
    end=start+1+nxt.start() if nxt else len(content)
    block=content[start:end]
    out=[]
    for line in block.splitlines():
        if re.search(r"OWNED PATHS|OWNED",line,re.I):
            parts=line.split(":",1)
            if len(parts)==2: out.extend(x.strip() for x in re.split(r"[,;]",parts[1]) if x.strip())
        elif re.match(r"^\s*[-*]\s+.+\s*$",line):
            value=line.strip().lstrip("-* ").strip()
            if "/" in value or value.startswith("."): out.append(value)
    return [norm(x) for x in out if norm(x)]

def validate_issue(repo, number):
    issue=api("/repos/%s/issues/%d"%(repo,number))
    body=issue.get("body") or ""
    lbls=labels(issue)
    if "agent" not in lbls:
        print("Agent readiness: skipped (no agent label)."); return
    missing=[x for x in REQUIRED if x not in sections(body)]
    if missing: fail("Issue #%d: missing required sections: %s"%(number,", ".join(missing)))
    for meta in ("agent","priority","deadline"):
        if meta not in lbls and not re.search(r"^\s*"+re.escape(meta)+r"\s*:",body,re.I|re.M):
            fail("Issue #%d: missing %s metadata."%(number,meta))
    roots=parse_owned(body) or work_map(repo,number)
    if not roots: fail("Issue #%d: no OWNED PATHS could be resolved."%number)
    for dep in [x for x in refs(body) if x != number]:
        d=api("/repos/%s/issues/%d"%(repo,dep))
        if "agent" in labels(d) and d.get("state") != "closed" and state_label(d) in BLOCKING_STATES:
            fail("Issue #%d: dependency #%d is blocking (%s)."%(number,dep,state_label(d)))
    active=api("/repos/%s/issues?state=open&per_page=100"%repo)
    collisions=[]
    for other in active:
        n=other.get("number")
        if n==number or "agent" not in labels(other) or state_label(other) not in ACTIVE_STATES: continue
        other_roots=work_map(repo,n)
        for a in roots:
            for b in other_roots:
                if owned(a,[b]) or owned(b,[a]): collisions.append((n,a,b))
    if collisions:
        fail("Issue #%d: active ownership collision: %s"%(number,"; ".join("#%d %s <-> %s"%x for x in collisions)))
    print("Agent readiness: READY for issue #%d."%number)

def linked_issue(pr):
    text=(pr.get("body") or "")+"\n"+(pr.get("title") or "")
    m=re.search(r"(?i)(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s+#(\d+)",text)
    if m: return int(m.group(1))
    r=refs(text)
    return r[0] if len(r)==1 else None

def validate_pr(repo, number):
    pr=api("/repos/%s/pulls/%d"%(repo,number))
    issue_number=linked_issue(pr)
    if issue_number is None: fail("PR #%d: no unambiguous linked issue. Use Closes/Fixes/Resolves #N."%number)
    issue=api("/repos/%s/issues/%d"%(repo,issue_number))
    if "agent" not in labels(issue): fail("PR #%d: linked issue #%d lacks agent label."%(number,issue_number))
    roots=parse_owned(issue.get("body") or "") or work_map(repo,issue_number)
    if not roots: fail("PR #%d: linked issue #%d has no OWNED PATHS."%(number,issue_number))
    files=api("/repos/%s/pulls/%d/files?per_page=100"%(repo,number))
    bad=[x["filename"] for x in files if not owned(x["filename"],roots)]
    if bad: fail("PR #%d: out-of-scope files: %s"%(number,", ".join(bad)))
    if state_label(issue) in BLOCKING_STATES: fail("PR #%d: linked issue #%d is blocking."%(number,issue_number))
    print("PR readiness: PASS for #%d; linked issue #%d."%(number,issue_number))

def main():
    with open(os.environ["GITHUB_EVENT_PATH"],encoding="utf-8") as f: event=json.load(f)
    repo=os.environ["GITHUB_REPOSITORY"]
    if os.environ["GITHUB_EVENT_NAME"]=="issues": validate_issue(repo,event["issue"]["number"])
    elif os.environ["GITHUB_EVENT_NAME"]=="pull_request": validate_pr(repo,event["pull_request"]["number"])
    else: fail("Unsupported event.")

if __name__=="__main__": main()
