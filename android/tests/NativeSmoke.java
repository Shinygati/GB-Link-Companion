package io.gblink.companion;
import java.nio.file.*;
import java.util.*;
/** Runs the exact JNI frontend and mGBA save hook, compiled for the host. */
public final class NativeSmoke {
    public static void main(String[] args)throws Exception{
        byte[] rom=Files.readAllBytes(Paths.get(args[0]));int[] frame=new int[240*160];short[] audio=new short[8192];
        int checks=0;
        for(int type=1;type<=6;type++){
            byte[] save=new byte[Wire.SIZES[type]];Arrays.fill(save,(byte)0x11);save[save.length-1]=0x6F;
            if(!Mgba.load(rom,save,type))throw new AssertionError("mGBA load type "+type);
            if(!Arrays.equals(save,Mgba.save()))throw new AssertionError("mGBA initial save mismatch type "+type);checks++;
            if(type==1){
                int saves=0;for(int i=0;i<180;i++){Mgba.step(0,frame,audio);saves+=Mgba.saveEvents();}
                byte[] after=Mgba.save();if(after[0]!=0x12)throw new AssertionError("Game did not read/modify cartridge SRAM: "+after[0]);checks++;
                if(after[after.length-1]!=0x6F)throw new AssertionError("Save tail changed");checks++;
                if(saves<1)throw new AssertionError("mGBA savedataUpdated hook never fired");checks++;
                if((frame[100]&0x00FF00)<0x008000)throw new AssertionError("Video frame not rendered");checks++;
                System.out.println("mGBA rendered green frame, read & wrote SRAM, emitted "+saves+" save event(s)");
            }
            Mgba.close();
        }
        System.out.println("PASS: "+checks+" real mGBA JNI / save-chip / video checks");
    }
}
