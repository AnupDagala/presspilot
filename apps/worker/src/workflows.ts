import {proxyActivities,sleep} from "@temporalio/workflow";
import type * as activities from "./activities";
import type {Run} from "./contracts";
const a=proxyActivities<typeof activities>({startToCloseTimeout:"60 seconds",heartbeatTimeout:"30 seconds",retry:{initialInterval:"1 second",maximumInterval:"8 seconds",maximumAttempts:3,nonRetryableErrorTypes:["FORBIDDEN","INVALID","NOT_FOUND","LIMIT","PERMANENT"]}});
const control=proxyActivities<Pick<typeof activities,"inspect"|"execute"|"escalateFailure">>({startToCloseTimeout:"15 seconds",retry:{initialInterval:"1 second",maximumInterval:"4 seconds",maximumAttempts:3}});
export async function resolveCase(run:Run):Promise<string>{
 try {
  let tick=0;
  for(let revision=0;revision<3;revision++){
   const result=await a.investigate(run,revision);
   if(result.status!=="awaiting_approval"||!result.proposal)return result.status;
   for(let wait=0;wait<720;wait++){
    await sleep("5 seconds");
    const state=await control.inspect(run,tick++);
    if(["completed","escalated"].includes(state.case.status))return state.case.status;
    if(Date.now()>Date.parse(result.proposal.expiresAt)){await control.escalateFailure(run,"Approval expired; human review required");return "escalated"}
    const outcome=await control.execute(run,result.proposal.id);
    if(outcome.status==="executed")return "completed";
    if(outcome.status==="rejected")return "escalated";
    if(outcome.status==="invalidated")break;
   }
  }
  await control.escalateFailure(run,"Reassessment limit reached; human review required");return "escalated";
 }catch{
  await control.escalateFailure(run,"Agent activity failed after bounded retries; operator review required");return "escalated";
 }
}
