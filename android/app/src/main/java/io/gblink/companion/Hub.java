package io.gblink.companion;

import android.app.*;
import android.content.*;
import android.hardware.usb.*;
import android.os.*;
import java.io.*;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicBoolean;

/** Process-scoped link owner. USB loss never owns or stops the emulation thread. */
final class Hub {
    private static Hub instance;
    static synchronized Hub get(Context c){if(instance==null)instance=new Hub(c.getApplicationContext());return instance;}
    final Context context;final Store store;final UsbManager usb;
    final ExecutorService worker=Executors.newSingleThreadExecutor();
    final Handler main=new Handler(Looper.getMainLooper());
    final AtomicBoolean syncQueued=new AtomicBoolean();
    final AtomicBoolean keysQueued=new AtomicBoolean();
    final Set<String> blocked=ConcurrentHashMap.newKeySet();
    volatile UsbPort port;volatile Wire wire;volatile UsbDevice device;
    volatile boolean ready,busy;volatile int gbaKeys;
    volatile String message="Connect GB-Link over USB OTG",lastError="";
    volatile String installedFirmware="not checked";
    volatile boolean firmwareOffer,bootloaderOffer,waitingForBootloader;
    volatile Runnable listener;volatile String activeId;
    interface Job { void run()throws Exception; }
    interface Done { void finish(Exception error); }
    private Hub(Context context){
        this.context=context;usb=(UsbManager)context.getSystemService(Context.USB_SERVICE);
        try{store=new Store(new File(context.getFilesDir(),"library"));}catch(IOException e){throw new IllegalStateException(e);}
        IntentFilter filter=new IntentFilter();filter.addAction("io.gblink.companion.USB_PERMISSION");filter.addAction(UsbManager.ACTION_USB_DEVICE_DETACHED);filter.addAction(UsbManager.ACTION_USB_DEVICE_ATTACHED);
        BroadcastReceiver receiver=new BroadcastReceiver(){public void onReceive(Context c,Intent intent){
            UsbDevice d=intent.getParcelableExtra(UsbManager.EXTRA_DEVICE);
            if("io.gblink.companion.USB_PERMISSION".equals(intent.getAction())){
                if(d!=null&&intent.getBooleanExtra(UsbManager.EXTRA_PERMISSION_GRANTED,false)){
                    if(Picoboot.matches(d))flashFirmware(d);else connect(d);
                }
                else{message="USB permission denied. Tap Connect to retry.";changed();}
            }else if(UsbManager.ACTION_USB_DEVICE_DETACHED.equals(intent.getAction())){
                if(d!=null&&device!=null&&d.getDeviceId()==device.getDeviceId())disconnect("GB-Link removed · keep playing; save write-back is pending");
            }else{
                if(d!=null&&Picoboot.matches(d)){message="RP2040 BOOTSEL detected · ready to install bundled GB-Link firmware";if(waitingForBootloader)bootloaderOffer=true;}
                else message="GB-Link attached · tap Connect to start the GBA helper";changed();
            }
        }};
        if(Build.VERSION.SDK_INT>=33)context.registerReceiver(receiver,filter,Context.RECEIVER_NOT_EXPORTED);else context.registerReceiver(receiver,filter);
    }
    void changed(){main.post(()->{Runnable r=listener;if(r!=null)r.run();});}
    List<UsbDevice> devices(){List<UsbDevice> out=new ArrayList<>();for(UsbDevice d:usb.getDeviceList().values())if(d.getVendorId()==0x2FE3)out.add(d);return out;}
    void connect(UsbDevice d){
        if(busy)return;
        if(!usb.hasPermission(d)){
            Intent i=new Intent("io.gblink.companion.USB_PERMISSION").setPackage(context.getPackageName());
            usb.requestPermission(d,PendingIntent.getBroadcast(context,0,i,PendingIntent.FLAG_IMMUTABLE|PendingIntent.FLAG_UPDATE_CURRENT));return;
        }
        run("Starting helper · switch on GBA holding START + SELECT",()->{
            disconnect("Connecting…");device=d;UsbPort p=new UsbPort(usb,d);port=p;Wire w=new Wire(p);wire=w;
            installedFirmware=w.firmwareVersion();
            if(!FirmwareImage.VERSION.equals(installedFirmware)){
                firmwareOffer=true;message="Firmware "+installedFirmware+" · bundled "+FirmwareImage.VERSION;return;
            }
            startHelper(w,p);
        },error->{if(error!=null)disconnect("Connection failed · "+error.getMessage());});
    }
    private void startHelper(Wire w,UsbPort p)throws IOException{
            try(InputStream in=context.getAssets().open("gblink_companion_mb.gba")){w.start(bytes(in,262144),progress("Sending GBA helper"));}
            if(port!=p||p.closed)throw new IOException("GB-Link was removed");
            ready=true;message="GB-Link connected · GBA helper ready";
    }
    void continueWithInstalledFirmware(){
        firmwareOffer=false;run("Starting helper with installed firmware",()->{
            Wire w=wire;UsbPort p=port;if(w==null||p==null)throw new IOException("Reconnect GB-Link first");startHelper(w,p);
        },e->{if(e!=null)disconnect("Connection failed · "+e.getMessage());});
    }
    List<UsbDevice> bootloaders(){List<UsbDevice> list=new ArrayList<>();for(UsbDevice d:usb.getDeviceList().values())if(Picoboot.matches(d))list.add(d);return list;}
    void enterBootloader(Done done){
        if(activeId!=null){if(done!=null)done.finish(new IOException("Exit the game before updating adapter firmware"));return;}
        run("Rebooting GB-Link into BOOTSEL",()->{
            Wire w=wire;if(w==null)throw new IOException("Connect GB-Link first, or hold BOOTSEL while plugging it into the phone");
            waitingForBootloader=true;w.commandFrame(0x43);disconnect("Waiting for RP2040 BOOTSEL · keep the USB cable connected");
        },done);
    }
    void installFirmware(UsbDevice d){
        if(busy||activeId!=null)return;
        bootloaderOffer=false;waitingForBootloader=false;
        if(!usb.hasPermission(d)){
            usb.requestPermission(d,PendingIntent.getBroadcast(context,0,new Intent("io.gblink.companion.USB_PERMISSION").setPackage(context.getPackageName()),PendingIntent.FLAG_IMMUTABLE|PendingIntent.FLAG_UPDATE_CURRENT));return;
        }
        flashFirmware(d);
    }
    private void flashFirmware(UsbDevice d){
        if(activeId!=null)return;
        run("Checking bundled RP2040 firmware",()->{
            FirmwareImage image;
            try(InputStream in=context.getAssets().open(FirmwareImage.FILE)){image=new FirmwareImage(bytes(in,1024*1024),FirmwareImage.SHA256);}
            File backups=new File(context.getFilesDir(),"firmware-backups");if(!backups.isDirectory()&&!backups.mkdirs())throw new IOException("Cannot create firmware backup folder");
            try(Picoboot boot=new Picoboot(usb,d)){
                image.install(boot,new File(backups,"rp2040-before-"+System.currentTimeMillis()+".gbfw"),progress("Installing / verifying GB-Link "+FirmwareImage.VERSION));
            }
            installedFirmware="awaiting reconnect verification";
            message="Firmware flash read-back verified · tap Connect after GB-Link reappears to check version";
        },e->{if(e!=null){lastError=e.getMessage();message="Firmware update incomplete · "+e.getMessage();}changed();});
    }
    void disconnect(String why){ready=false;gbaKeys=0;UsbPort p=port;port=null;wire=null;device=null;if(p!=null)p.close();message=why;changed();}
    void run(String label,Job job,Done done){
        if(busy){if(done!=null)done.finish(new IOException("A cartridge operation is already running"));return;}
        busy=true;gbaKeys=0;message=label;changed();
        worker.execute(()->{
            Exception error=null;try{job.run();}catch(Exception e){error=e;lastError=e.getMessage();}
            busy=false;Exception result=error;main.post(()->{if(done!=null)done.finish(result);changed();});
        });
    }
    Wire require()throws IOException{Wire w=wire;if(!ready||w==null)throw new IOException("Reconnect GB-Link, start the GBA holding START + SELECT, and tap Connect.");return w;}
    Wire.Progress progress(String label){return (d,t)->{if(t>0)message=label+" · "+Math.min(100,(int)(100L*d/t))+"%";changed();};}
    void sync(String id,String force,Done done){
        run("Checking cartridge before save write-back",()->{
            Wire w=require();CartridgeSync.write(store,id,w,force,progress("Writing / verifying cartridge save"));
            blocked.remove(id);message=store.entry(id).pending()?"A newer save is pending":"Save verified on cartridge";
        },error->{if(error!=null){blocked.add(id);message="Save retained on phone · "+error.getMessage();}if(done!=null)done.finish(error);});
    }
    void autoSync(String id){
        if(!ready||busy||blocked.contains(id)||!syncQueued.compareAndSet(false,true))return;
        main.post(()->{
            try{
                if(!ready||busy||!store.entry(id).pending()){syncQueued.set(false);return;}
                sync(id,null,e->{syncQueued.set(false);});
            }catch(IOException e){syncQueued.set(false);lastError=e.getMessage();changed();}
        });
    }
    void pollKeys(){
        if(!context.getSharedPreferences("settings",0).getBoolean("gbaController",true)){gbaKeys=0;return;}
        if(!ready||busy||!keysQueued.compareAndSet(false,true))return;
        worker.execute(()->{try{if(busy||!ready)return;gbaKeys=require().keys();}catch(IOException e){gbaKeys=0;ready=false;message="GBA link unavailable · phone controls remain active";changed();}finally{keysQueued.set(false);}});
    }
    static byte[] bytes(InputStream in,int maximum)throws IOException{
        ByteArrayOutputStream out=new ByteArrayOutputStream();byte[] b=new byte[8192];int n;
        while((n=in.read(b))!=-1){if(out.size()+n>maximum)throw new IOException("File is too large");out.write(b,0,n);}return out.toByteArray();
    }
}
