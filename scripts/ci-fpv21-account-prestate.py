#!/usr/bin/env python3
"""Synthetic schema47 account prestate for the disposable installed CI lane."""
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import ssl
import stat
import sys
import urllib.error
import urllib.parse
import urllib.request

def private_read(path):
    path=Path(path);mode=path.stat()
    if not stat.S_ISREG(mode.st_mode) or mode.st_nlink!=1 or stat.S_IMODE(mode.st_mode)&0o077:
        raise ValueError("private fixture file required")
    return path.read_text()

class Client:
    def __init__(self, origin, ca, expected_port=9443):
        u=urllib.parse.urlsplit(origin)
        if (u.scheme,u.hostname,u.port,u.path,u.query,u.fragment)!=("https","127.0.0.1",expected_port,"","",""):
            raise ValueError("isolated fixed HTTPS fixture required")
        self.origin=origin;self.csrf=""
        self.opener=urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=ca)),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def call(self,path,data=None,expected=200):
        headers={"Origin":self.origin,"Content-Type":"application/json","Accept":"application/json"}
        if self.csrf:headers["X-CSRF-Token"]=self.csrf
        request=urllib.request.Request(self.origin+path,data=None if data is None else json.dumps(data).encode(),headers=headers)
        try:
            with self.opener.open(request,timeout=15) as response:status=response.status;raw=response.read(131073)
        except urllib.error.HTTPError as response:status=response.code;raw=response.read(131073)
        if status!=expected or len(raw)>131072:raise ValueError("native account prestate HTTP contract")
        return json.loads(raw)
    def login(self,username,password):
        reply=self.call("/api/v1/access/login",{"username":username,"password":password})
        self.csrf=reply["csrf_token"]
        return self.call("/api/v1/access/session")

def token(reply,origin):
    u=urllib.parse.urlsplit(reply["activation_link"])
    if u.scheme+"://"+u.netloc!=origin or u.path!="/access/activate" or not u.fragment.startswith("xi-ac1_"):raise ValueError("holder link binding")
    return u.fragment

def prepare(client,password):
    accounts=client.call("/api/v1/access/accounts")["items"]
    if any(a["username"]=="upgrade.reader" for a in accounts):raise ValueError("fresh fixture username required")
    invite=client.call("/api/v1/access/accounts",{"action":"invite","username":"upgrade.reader","role":"workbench_investigation_reader","expected_revision":0,"password":password})
    subject=invite["account"]["subject_id"];holder_password=secrets.token_urlsafe(32)
    spent=[token(invite,client.origin)]
    def set_password(value):
        reply=client.call("/api/v1/access/account-password",{"token":value,"password":holder_password,"confirmation":holder_password})
        if reply!={"username":"upgrade.reader","status":"password_set_sign_in_required"}:raise ValueError("holder completion receipt")
    set_password(spent[0])
    def recover():
        account=next(a for a in client.call("/api/v1/access/accounts")["items"] if a["subject_id"]==subject)
        return client.call("/api/v1/access/accounts",{"action":"recover","subject_id":subject,"expected_revision":account["revision"],"password":password})
    recovered=recover();spent.append(token(recovered,client.origin));set_password(spent[-1])
    pending=recover();account=pending["account"]
    if account["subject_id"]!=subject or account["revision"]<=3 or not account["needs_activation"]:raise ValueError("durable recovery prestate absent")
    return {"subject":subject,"username":"upgrade.reader","revision":account["revision"],"holder_password":holder_password,"spent":spent,"pending":token(pending,client.origin)}

def verify(client,state):
    account=next(a for a in client.call("/api/v1/access/accounts")["items"] if a["subject_id"]==state["subject"])
    if account["username"]!=state["username"] or account["revision"]!=state["revision"] or not account["needs_activation"]:raise ValueError("preserved pending account differs")
    for value in state["spent"]:
        client.call("/api/v1/access/account-password",{"token":value,"password":state["holder_password"],"confirmation":state["holder_password"]},expected=400)
    reply=client.call("/api/v1/access/account-password",{"token":state["pending"],"password":state["holder_password"],"confirmation":state["holder_password"]})
    if reply["username"]!=state["username"]:raise ValueError("preserved pending ticket target differs")
    client.call("/api/v1/access/account-password",{"token":state["pending"],"password":state["holder_password"],"confirmation":state["holder_password"]},expected=400)
    session=client.login(state["username"],state["holder_password"])["session"]
    if session["subject_id"]!=state["subject"] or session["role"]!="workbench_investigation_reader":raise ValueError("recovered account sign-in differs")
    return {"schema_version":"xinsight.ci-schema47-account-prestate.v1","classification":"ci_synthetic_not_target","subject_preserved":True,"preexisting_recovery_revision_preserved":True,"spent_tickets_refused":len(state["spent"]),"pending_ticket_completed_once":True,"normal_login_after_upgrade":True,"new_project_grants":0}

def main():
    if os.geteuid()!=0 or os.environ.get("GITHUB_ACTIONS")!="true" or os.environ.get("XINSIGHT_CI_EPHEMERAL_HOST")!="1" or len(sys.argv)!=4 or sys.argv[1] not in ("prepare","verify"):
        raise ValueError("explicit disposable CI root fixture required")
    action,input_file,state_file=sys.argv[1:];installed=json.loads(private_read(input_file))
    if installed["schema_version"]!="xinsight.ci-installed-joint.v1":raise ValueError("installed fixture contract")
    client=Client(installed["xi_origin"],installed["xi_ca_file"]);password=private_read(installed["xi_admin_password_file"]).strip()
    session=client.login(installed["xi_admin_username"],password)["session"]
    owner=next(a for a in client.call("/api/v1/access/accounts")["items"] if a["username"]==installed["xi_admin_username"])
    if session["subject_id"]!=owner["subject_id"] or session["role"]!="platform_admin":raise ValueError("native fixture owner identity")
    path=Path(state_file)
    if action=="prepare":
        state=prepare(client,password)
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,"w") as f:json.dump(state,f)
        print("schema47_account_prestate=prepared recovered_existing_account=true tickets=nonempty project_grants=unchanged")
    else:
        result=verify(client,json.loads(private_read(path)))
        print(json.dumps(result,sort_keys=True))

if __name__=="__main__":
    try:main()
    except Exception:
        raise SystemExit("schema47_account_prestate=failed")
