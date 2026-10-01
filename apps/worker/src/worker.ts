import {Worker,NativeConnection} from "@temporalio/worker";
import * as activities from "./activities";
async function main(){
 const connection=await NativeConnection.connect({address:process.env.TEMPORAL_ADDRESS??"127.0.0.1:7233"});
 const worker=await Worker.create({connection,namespace:process.env.TEMPORAL_NAMESPACE??"default",taskQueue:"presspilot",workflowsPath:require.resolve("./workflows"),activities,maxConcurrentActivityTaskExecutions:2,maxConcurrentWorkflowTaskExecutions:4,shutdownGraceTime:"10 seconds"});
 process.on("SIGINT",()=>worker.shutdown());process.on("SIGTERM",()=>worker.shutdown());await worker.run();await connection.close();
}
main().catch(()=>{console.error(JSON.stringify({event:"worker_failed",message:"Worker startup or runtime failed; consult Temporal task status"}));process.exitCode=1});
