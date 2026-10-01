import { readFileSync } from "node:fs";
import { ApplicationFailure } from "@temporalio/activity";
import type {Run} from "./contracts";
export function secret(name:string):string {
 const path=process.env[name+"_FILE"];return path?readFileSync(path,"utf8").trim():process.env[name]??"";
}
export async function call<T>(run:Run,callId:string,name:string,args:Record<string,unknown>={}):Promise<T>{
 const response=await fetch((process.env.BUSINESS_API??"http://127.0.0.1:8080")+"/internal/tools",{
 method:"POST",headers:{"Content-Type":"application/json",Authorization:"Bearer "+secret("WORKER_SECRET")},
 body:JSON.stringify({...run,mode:undefined,callId,name,args}),signal:AbortSignal.timeout(10000)
 });
 const text=await response.text();if(text.length>32768)throw ApplicationFailure.nonRetryable("Backend output exceeded limit","LIMIT");
 const out=JSON.parse(text);if(!response.ok){
  if(response.status>=500)throw ApplicationFailure.retryable("Backend temporarily unavailable","RETRYABLE");
  throw ApplicationFailure.nonRetryable(String(out.error??"Tool rejected"),String(out.code??"PERMANENT"));
 }return out as T;
}
