package io.gblink.companion;
import java.io.*;
import java.util.*;

/** Only this class performs writes: back up, journal, write, read back, then commit. */
final class CartridgeSync {
    static final class Conflict extends IOException {
        final String currentHash;
        Conflict(String hash){super("The cartridge save changed since this phone session began. Both copies are retained. Choose whether to replace the cartridge save.");currentHash=hash;}
    }
    static void write(Store store,String id,Wire wire,String forceHash,Wire.Progress progress)throws IOException{
        Store.Entry e=store.entry(id);byte[] target=store.data(e);String wanted=Store.hash(target);
        Wire.Cart cart=wire.cart();match(e,cart);
        byte[] old=wire.readSave(e.type,progress);String current=Store.hash(old);
        store.backup(id,"cartridge-read",old);
        if(current.equals(wanted)){store.complete(id,wanted);return;}
        if(!e.accepts(current)&&!current.equals(forceHash))throw new Conflict(current);
        store.backup(id,"before-write",old);
        int total=0;for(int off=0;off<old.length;off+=4096){int end=Math.min(off+4096,old.length);if(!Arrays.equals(Arrays.copyOfRange(old,off,end),Arrays.copyOfRange(target,off,end)))total++;}
        byte[] predicted=old.clone();int step=0;
        // Recheck the header immediately before the first destructive command.
        match(e,wire.cart());
        for(int off=0;off<old.length;off+=4096){
            int end=Math.min(off+4096,old.length);byte[] b=Arrays.copyOfRange(target,off,end);
            if(Arrays.equals(Arrays.copyOfRange(old,off,end),b))continue;
            System.arraycopy(b,0,predicted,off,b.length);
            store.prepareBlock(id,predicted); // durable before sending the write
            wire.writeBlock(e.type,off,b,step++,total);progress.update(step,total);
        }
        match(e,wire.cart());
        byte[] verified=wire.readSave(e.type,progress);
        store.backup(id,"cartridge-verify",verified);
        if(!Arrays.equals(verified,target))throw new IOException("Read-back verification failed. Phone save and pre-write backup are retained. Reconnect and retry.");
        store.complete(id,wanted);
    }
    static void match(Store.Entry e,Wire.Cart c)throws IOException{
        if(c.type!=e.type||!c.identity().equals(e.cart))throw new IOException("Wrong cartridge or save chip. Insert "+e.title+" ("+e.code+").");
    }
}
