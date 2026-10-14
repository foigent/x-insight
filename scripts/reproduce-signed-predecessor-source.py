#!/usr/bin/env python3
"""Admit fixed original predecessor sources and run their exact build recipes."""
import argparse,hashlib,json,os,platform,shlex,shutil,stat,subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parent.parent
SPECS={'rc13': {'origin_commit': '8bcea5bebf6f0f0796391dfe8c62fdf7a43519eb', 'go': 'go1.25.11', 'profile': 'rc13', 'manifest_sha256': '80fa1aad7cdf754912bf95907a6d7e404d3570c0d9b8b25ba5e6c652fb440c5b'}, 'fpv11': {'origin_commit': '9c58e07f011918c4a0aed2f876ce4baa9fb12b5e', 'go': 'go1.25.12', 'profile': 'development-d', 'manifest_sha256': 'a28706219000d441df9415622e55de392ee3c898dd001752371f60598f559f0f'}, 'fpv14': {'origin_commit': 'a10aa9cf517ce187a95f5c7e20be0f8c8fdbe6c6', 'go': 'go1.25.12', 'profile': 'development-d', 'manifest_sha256': 'acd45c086ab84904780e662be604855c419486446bd9f76a4ccb4420fb085555'}, 'fpv17': {'origin_commit': '164582cab0e7f169c265c2494d04920c61af833d', 'go': 'go1.25.12', 'profile': 'development-d', 'manifest_sha256': 'cd63b7665b0952e5d1c365e1f654bc8520ae8dc1e6d49bac93c6fef1e5cc808c'}}
def require(ok,code):
 if not ok:raise ValueError(code)
def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def manifest(role):
 spec=SPECS[role];p=ROOT/'compatibility'/(role+'-linux-amd64')/'source-manifest.json';s=p.lstat()
 require(stat.S_ISREG(s.st_mode) and s.st_nlink==1 and sha(p)==spec['manifest_sha256'],'manifest_digest')
 m=json.loads(p.read_bytes());require(m['role']==role and m['origin_commit']==spec['origin_commit'] and m['required_go']==spec['go'] and m['builder_profile']==spec['profile'],'manifest_origin')
 return m
def generated(name):
 return name=='frontend/node_modules' or name.startswith('frontend/node_modules/') or (name.startswith('frontend/') and '/' not in name[len('frontend/'):] and name.endswith('.tsbuildinfo'))
def check(role,source,allow_generated=False,embedded_only=False):
 require(source.is_absolute() and source.resolve()==source and not source.is_symlink() and source.is_dir(),'source_directory')
 m=manifest(role);expected={f['path']:f for f in m['files']}
 if embedded_only:expected={k:v for k,v in expected.items() if k.startswith(('internal/webui/dist/','internal/webui/manual/zh-CN/'))}
 dirs={str(p) for name in expected for p in Path(name).parents if str(p)!='.'};actual=set()
 if not embedded_only:
  for directory,children,names in os.walk(source,followlinks=False):
   for name in list(children):
    p=Path(directory)/name;n=str(p.relative_to(source))
    if allow_generated and n=='frontend/node_modules':require(not p.is_symlink(),'generated_root_link');children.remove(name);continue
    require(not p.is_symlink() and n in dirs,'source_extra_or_link')
   for name in names:
    n=str((Path(directory)/name).relative_to(source))
    if allow_generated and generated(n):continue
    actual.add(n)
  require(actual==set(expected),'source_file_set')
 for name,f in expected.items():
  p=source/name;s=p.lstat();require(stat.S_ISREG(s.st_mode) and s.st_nlink==1,'source_not_regular')
  require(bool(s.st_mode&0o111)==(f['git_mode']=='100755') and s.st_size==f['bytes'] and sha(p)==f['sha256'],'source_mode_size_digest')
  after=p.lstat();require((s.st_dev,s.st_ino,s.st_size,s.st_mtime_ns)==(after.st_dev,after.st_ino,after.st_size,after.st_mtime_ns),'source_changed')
 return m
def replace_once(text,old,new):
 require(text.count(old)==1,'original_recipe_shape');return text.replace(old,new)
def recipe(role,source,m,verify=False):
 name='verify-signed-release.sh' if verify else 'build-signed-release.sh';text=(source/'scripts'/name).read_text()
 text=replace_once(text,'ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"','ROOT="$(pwd)"')
 text=replace_once(text,'git rev-parse HEAD','printf %s '+shlex.quote(m['origin_commit']))
 if not verify:
  block='if [ -n "$(git status --porcelain -- internal/webui/manual/zh-CN internal/webui/dist)" ]; then\n  echo "generated manual or frontend assets differ from the committed release sources" >&2\n  git status --short -- internal/webui/manual/zh-CN internal/webui/dist >&2\n  exit 1\nfi'
  text=replace_once(text,block,'python3 '+shlex.quote(str(Path(__file__).resolve()))+' --check-embedded-source '+role+' "$ROOT"')
  clean='GIT_OPTIONAL_LOCKS=0 git status --porcelain' if role!='rc13' else 'git status --porcelain'
  text=replace_once(text,clean,"printf ''")
  text=replace_once(text,'git show -s --format=%ct "$COMMIT"','printf %s '+str(m['origin_source_date_epoch']))
  text=replace_once(text,'git ls-files migrations schemas fixtures',r"printf '%s\n' "+' '.join(shlex.quote(n) for n in m['package_input_paths']))
 require('$(git ' not in text and 'git status' not in text and '\x00' not in text,'original_recipe_git_remaining')
 return text
def fresh(path):require(path.is_absolute() and path.resolve()==path and not path.exists() and not path.is_symlink(),'output_not_fresh')
def run_recipe(role,source,args,verify=False):
 m=check(role,source,allow_generated=verify)
 env=dict(os.environ,GOTOOLCHAIN='local',GOFLAGS='-mod=readonly',XINSIGHT_EXPECT_GO_VERSION=SPECS[role]['go'])
 subprocess.run(['bash','-c',recipe(role,source,m,verify),'original-'+role+'-source-recipe',*map(str,args)],cwd=source,env=env,check=True)
 check(role,source,allow_generated=verify or not ('--dry-run' in args))
def main():
 p=argparse.ArgumentParser(description=__doc__);g=p.add_mutually_exclusive_group(required=True)
 for option in ['check-source-directory','check-built-source-directory','check-embedded-source','dry-run','build','verify']:g.add_argument('--'+option,action='store_true')
 p.add_argument('role',choices=sorted(SPECS));p.add_argument('paths',nargs='+',type=Path);a=p.parse_args();role=a.role
 try:
  if a.check_source_directory or a.check_built_source_directory or a.check_embedded_source:
   require(len(a.paths)==1,'arguments');m=check(role,a.paths[0],allow_generated=a.check_built_source_directory,embedded_only=a.check_embedded_source)
   print(json.dumps({'role':role,'origin_commit':m['origin_commit'],'selected_files':len(m['files']),'passed':True},sort_keys=True))
  elif a.dry_run:
   require(role!='rc13' and len(a.paths)==2,'original_dry_run_unavailable_or_arguments');source,out=a.paths;fresh(out)
   run_recipe(role,source,[str(out),'--profile=development-d','--dry-run'])
   require(not out.exists(),'dry_run_created_output')
  elif a.build:
   require(len(a.paths)==3,'arguments');source,output_source,output_dist=a.paths;check(role,source)
   require(platform.system()=='Linux' and platform.machine() in ['x86_64','amd64'],'native_linux_amd64_required')
   fresh(output_source);fresh(output_dist)
   require(not output_source.is_relative_to(source) and not output_dist.is_relative_to(source) and not output_source.is_relative_to(output_dist) and not output_dist.is_relative_to(output_source),'output_overlap')
   shutil.copytree(source,output_source);args=[str(output_dist)]+([] if role=='rc13' else ['--profile=development-d'])
   run_recipe(role,output_source,args)
  else:
   require(len(a.paths)==4,'arguments');source,artifact,trust,destination=a.paths;fresh(destination)
   require(platform.system()=='Linux' and platform.machine() in ['x86_64','amd64'],'native_linux_amd64_required')
   args=[str(artifact),str(trust),str(destination)]+([] if role=='rc13' else ['--profile=development-d']);run_recipe(role,source,args,verify=True)
 except (OSError,ValueError,KeyError,TypeError,subprocess.SubprocessError) as e:
  raise SystemExit('Signed predecessor source refused: '+(str(e) if isinstance(e,ValueError) else 'input_or_original_recipe_failed'))
if __name__=='__main__':main()
