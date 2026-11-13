import {render,screen,waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach,expect,it,vi} from "vitest";
import {MeetingEditor} from "./meeting-editor";
import {saveMeeting} from "@/lib/api";
import type {Appointment} from "@/lib/portal-types";
vi.mock("@/lib/api",()=>({saveMeeting:vi.fn()}));
const initial={id:"synthetic-appointment",doctor_id:"doctor-a",status:"confirmed",meeting_url:"https://meet.example.invalid/original"} as Appointment;
beforeEach(()=>{vi.resetAllMocks();vi.mocked(saveMeeting).mockResolvedValue({ok:true});});
it("refreshes a pristine mounted editor after reassignment and saves the cleared value",async()=>{
 const saved=vi.fn();const {rerender}=render(<MeetingEditor appointment={initial} onSaved={saved}/>);
 rerender(<MeetingEditor appointment={{...initial,doctor_id:"doctor-b",meeting_url:""}} onSaved={saved}/>);
 expect(screen.getByLabelText("Meeting link")).toHaveValue("");
 await userEvent.click(screen.getByRole("button",{name:"Save link"}));
 await waitFor(()=>expect(saveMeeting).toHaveBeenCalledWith(initial.id,"",expect.objectContaining({doctor_id:"doctor-b",meeting_url:""})));
});
it("preserves an active draft but blocks stale saves until the current link is reviewed",async()=>{
 const saved=vi.fn();const {rerender}=render(<MeetingEditor appointment={initial} onSaved={saved}/>);
 await userEvent.clear(screen.getByLabelText("Meeting link"));
 await userEvent.type(screen.getByLabelText("Meeting link"),"https://meet.example.invalid/draft");
 rerender(<MeetingEditor appointment={{...initial,meeting_url:"https://meet.example.invalid/external"}} onSaved={saved}/>);
 expect(screen.getByLabelText("Meeting link")).toHaveValue("https://meet.example.invalid/draft");
 expect(screen.getByRole("button",{name:"Save link"})).toBeDisabled();
 expect(screen.getByRole("alert")).toHaveTextContent("changed");
 await userEvent.click(screen.getByRole("button",{name:"Use current link"}));
 expect(screen.getByLabelText("Meeting link")).toHaveValue("https://meet.example.invalid/external");
 await userEvent.click(screen.getByRole("button",{name:"Save link"}));
 await waitFor(()=>expect(saveMeeting).toHaveBeenCalledWith(initial.id,"https://meet.example.invalid/external",expect.objectContaining({meeting_url:"https://meet.example.invalid/external"})));
});
it("keeps a draft on unchanged refresh and follows a pristine external update",async()=>{
 const saved=vi.fn();const {rerender}=render(<MeetingEditor appointment={initial} onSaved={saved}/>);
 rerender(<MeetingEditor appointment={{...initial,meeting_url:"https://meet.example.invalid/new"}} onSaved={saved}/>);
 expect(screen.getByLabelText("Meeting link")).toHaveValue("https://meet.example.invalid/new");
 await userEvent.type(screen.getByLabelText("Meeting link"),"-draft");
 rerender(<MeetingEditor appointment={{...initial,meeting_url:"https://meet.example.invalid/new"}} onSaved={saved}/>);
 expect(screen.getByLabelText("Meeting link")).toHaveValue("https://meet.example.invalid/new-draft");
 expect(screen.getByRole("button",{name:"Save link"})).toBeEnabled();
});
