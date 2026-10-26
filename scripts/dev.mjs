import { spawn, spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
if(existsSync('.env')){
  for(const line of readFileSync('.env','utf8').split('\n')){
    const match=line.match(/^([A-Z_]+)=(.*)$/);
    if(match && process.env[match[1]]===undefined) process.env[match[1]]=match[2].replace(/^(['"])(.*)\1$/,'$2');
  }
}
const binary = resolve('backend/roadmap');
const portArgument = process.argv.indexOf('--port');
const port = portArgument >= 0 ? process.argv[portArgument + 1] : (process.env.PORT || '5173');
if (!/^\d+$/.test(port || '') || Number(port) < 1 || Number(port) > 65535) throw new Error('Invalid HTTP port');
if(!existsSync(binary)){
  const go=process.env.ROADMAP_GO || (existsSync('/workspace/tools/go/bin/go')?'/workspace/tools/go/bin/go':'go');
  const result=spawnSync(go,['build','-o',binary,'./cmd/roadmap'],{cwd:resolve('backend'),stdio:'inherit'});
  if(result.error) throw result.error;
  if(result.status) process.exit(result.status);
}
if (!process.env.ROADMAP_PASSWORD) throw new Error('Set ROADMAP_PASSWORD in .env or environment to a unique password with at least 12 characters');
const child = spawn(binary, [], {stdio:'inherit', env:{...process.env, ROADMAP_ADDR: `0.0.0.0:${port}`, ROADMAP_DB: process.env.ROADMAP_DB || resolve('backend/data/roadmap.db'), ROADMAP_STATIC:resolve('frontend/dist'), ROADMAP_COOKIE_SECURE:process.env.ROADMAP_COOKIE_SECURE || 'false'}});
for (const signal of ['SIGINT','SIGTERM']) process.on(signal,()=>child.kill(signal));
child.on('exit',code=>process.exit(code || 0));
