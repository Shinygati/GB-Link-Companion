package io.gblink.companion;

import android.app.*;
import android.content.*;
import android.hardware.usb.*;
import android.net.Uri;
import android.os.*;
import android.view.*;
import android.widget.*;
import java.io.*;
import java.util.*;

public final class MainActivity extends Activity {
    Hub hub;LinearLayout body;TextView connection;String tab="Games",prompted="";
    boolean wasBusy,showingPrompt,showingFirmware;volatile boolean cancelDump;
    byte[] exportBytes;File exportFile;
    static final int IMPORT_ROM=10,IMPORT_SAVE=11,EXPORT=12;
    @Override public void onCreate(Bundle b){super.onCreate(b);hub=Hub.get(this);getWindow().setStatusBarColor(Ui.BG);}
    @Override public void onResume(){super.onResume();prompted="";hub.listener=()->{
        if(connection!=null)connection.setText(hub.message);
        if(wasBusy&&!hub.busy){wasBusy=false;render();maybePrompt();}else wasBusy=hub.busy;
        if(!hub.busy)maybeFirmwarePrompt();
    };render();getWindow().getDecorView().postDelayed(()->{maybeFirmwarePrompt();maybePrompt();},400);}
    @Override public void onPause(){hub.listener=null;super.onPause();}
    void render(){
        if(isFinishing())return;
        ScrollView scroll=new ScrollView(this);scroll.setFillViewport(true);body=Ui.column(this);int p=Ui.dp(this,20);body.setPadding(p,p,p,p);scroll.addView(body);
        scroll.setOnApplyWindowInsetsListener((v,insets)->{body.setPadding(p+insets.getSystemWindowInsetLeft(),p+insets.getSystemWindowInsetTop(),p+insets.getSystemWindowInsetRight(),p+insets.getSystemWindowInsetBottom());return insets;});setContentView(scroll);
        ImageView icon=new ImageView(this);icon.setImageResource(io.gblink.companion.R.drawable.companion_icon);body.addView(icon,new LinearLayout.LayoutParams(Ui.dp(this,54),Ui.dp(this,54)));
        body.addView(Ui.text(this,"GB-LINK  /  ANDROID",12,Ui.ACCENT));body.addView(Ui.heading(this,"GB-Link Companion",28));
        body.addView(Ui.text(this,"Your ROM. Your cartridge save. Powered by mGBA.",14,Ui.MUTED));
        LinearLayout link=Ui.card(this,body);connection=Ui.text(this,hub.message,14,hub.ready?Ui.GREEN:Ui.MUTED);link.addView(connection);
        link.addView(Ui.button(this,hub.ready?"Reconnect / restart helper":"Connect GB-Link",this::connect));
        LinearLayout nav=new LinearLayout(this);for(String name:new String[]{"Games","Cartridge tools","Settings"}){
            Button b=Ui.button(this,name,()->{tab=name;render();});if(name.equals(tab))b.setTextColor(Ui.GREEN);nav.addView(b,new LinearLayout.LayoutParams(0,-2,1));
        }body.addView(nav);
        try{pendingCards();if(tab.equals("Games"))games();else if(tab.equals("Cartridge tools"))tools();else settings();}
        catch(IOException e){Ui.error(this,e);}
        body.addView(Ui.text(this,"Unofficial companion · 0.1.0 alpha\nGBA / GBA SP + GB-Link + GBC link cable + USB OTG",12,Ui.MUTED));
    }
    void pendingCards()throws IOException{
        for(Store.Entry e:hub.store.pending()){
            LinearLayout c=Ui.card(this,body);c.addView(Ui.heading(this,"Save awaiting cartridge",17));c.addView(Ui.text(this,e.title+" · "+e.code+"\nYour latest save is safe on this phone.",14,Ui.GREEN));
            c.addView(Ui.button(this,"Write back / resume / export",()->pendingMenu(e)));
        }
    }
    void games()throws IOException{
        body.addView(Ui.heading(this,"Your games",23));body.addView(Ui.button(this,"＋  Add ROM from phone storage",()->pick(IMPORT_ROM)));
        List<File> games=hub.store.games();
        if(games.isEmpty()){
            LinearLayout c=Ui.card(this,body);c.addView(Ui.heading(this,"Bring your cartridge to the big screen",20));
            c.addView(Ui.text(this,"Add a .gba ROM from phone storage, connect GB-Link, then tap Play. Each new session reads its save from the matching real cartridge.\n\nYou can unplug GB-Link once the game starts. Reconnect to write your save back.",16,Ui.MUTED));
        }
        for(File f:games){
            byte[] hdr=new byte[192];try(InputStream in=new FileInputStream(f)){if(in.read(hdr)!=192)continue;}
            String title=Wire.text(hdr,0xA0,12),code=Wire.text(hdr,0xAC,4);LinearLayout c=Ui.card(this,body);
            c.addView(Ui.heading(this,title,20));c.addView(Ui.text(this,code+"  ·  "+String.format(Locale.ROOT,"%.1f MB",f.length()/1048576.0)+"  ·  mGBA",13,Ui.MUTED));
            c.addView(Ui.button(this,"Play with cartridge save",()->play(f)));
        }
    }
    void tools(){
        body.addView(Ui.heading(this,"Cartridge tools",23));
        body.addView(Ui.text(this,"Connect and start the GBA helper before using a tool. Save writes keep a phone backup and are read back to verify.",14,Ui.MUTED));
        tool("Back up cartridge save","Export a raw .sav from the real save chip.",this::backup);
        tool("Write save to cartridge","Import an exact-size .sav, back up the existing save, then write and verify.",()->pick(IMPORT_SAVE));
        tool("Dump cartridge ROM","Read and checksum the cartridge ROM; add it to your phone library and optionally export it.",this::dump);
        tool("Export an earlier backup","Cartridge reads and pre-write copies stay in the app until you export them.",this::backups);
        if(hub.busy)body.addView(Ui.button(this,"Cancel ROM dump after current block",()->cancelDump=true));
    }
    void tool(String title,String detail,Runnable run){LinearLayout c=Ui.card(this,body);c.addView(Ui.heading(this,title,18));c.addView(Ui.text(this,detail,14,Ui.MUTED));c.addView(Ui.button(this,title,run));}
    void settings(){
        body.addView(Ui.heading(this,"Settings",23));Switch control=new Switch(this);control.setText("Use GBA buttons while connected");control.setTextColor(Ui.TEXT);
        control.setChecked(getSharedPreferences("settings",0).getBoolean("gbaController",true));control.setOnCheckedChangeListener((b,value)->getSharedPreferences("settings",0).edit().putBoolean("gbaController",value).apply());body.addView(control);
        body.addView(Ui.text(this,"Adapter firmware: "+hub.installedFirmware+"\nBundled official RP2040 firmware: "+FirmwareImage.VERSION,14,Ui.GREEN));
        body.addView(Ui.button(this,"Install bundled GB-Link firmware",this::firmware));
        body.addView(Ui.button(this,"Export bundled .uf2 for manual recovery",()->{try(InputStream in=getAssets().open(FirmwareImage.FILE)){exportBytes=Hub.bytes(in,1024*1024);exportFile=null;export(FirmwareImage.FILE);}catch(IOException e){Ui.error(this,e);}}));
        body.addView(Ui.text(this,"Phone touch controls and Android gamepads work with or without GB-Link.\n\nSave policy: cartridge at the start of every new session; write-back after in-game saves while connected; durable pending save while unplugged. Exit and the main screen prompt you to reconnect.\n\nUse in-game Save. Emulator save states are not cartridge saves. Uninstalling this app removes unexported phone saves and backups.",15,Ui.MUTED));
        body.addView(Ui.button(this,"About / source and licenses",()->new AlertDialog.Builder(this).setTitle("GB-Link Companion Android").setMessage("An Android port based on Shinygati/GB-Link-Companion, commit 23638b2a741672db86f02402c54b76e7c57f61a3.\n\nAndroid code and original helper: GPL-3.0-or-later. mGBA 0.10.5: Mozilla Public License 2.0. Notices are included in the APK assets and source archive.\n\nThis alpha supports GBA play, save read/write, cartridge ROM dumping and GBA controller input. Pokémon status screens are omitted. GB/GBC cartridge mode, Pokémon Bank, Wonder Cards and flash-cart ROM programming have not been ported.\n\nNot affiliated with GB-Link or mGBA.").setPositiveButton("OK",null).show()));
    }
    void connect(){
        if(hub.busy){toast("Wait for the current cartridge operation");return;}
        List<UsbDevice> ds=hub.devices();if(ds.isEmpty()){
            new AlertDialog.Builder(this).setTitle("Connect your GB-Link").setMessage("Connect the GB-Link adapter to the phone with a USB OTG cable. Connect the GBA / GBA SP using a Game Boy Color link cable. Insert the cartridge and switch on the GBA while holding START + SELECT, then tap Connect again.").setPositiveButton("OK",null).show();return;
        }
        if(ds.size()==1){hub.connect(ds.get(0));return;}
        String[] labels=new String[ds.size()];for(int i=0;i<ds.size();i++)labels[i]="GB-Link · USB device "+ds.get(i).getDeviceId();
        new AlertDialog.Builder(this).setTitle("Select GB-Link").setItems(labels,(d,i)->hub.connect(ds.get(i))).show();
    }
    void play(File file){
        if(hub.busy)return;
        String id=Store.id(file);
        try{if(hub.store.meta(id).exists()&&hub.store.entry(id).pending()){pendingMenu(hub.store.entry(id));return;}}
        catch(IOException e){Ui.error(this,e);return;}
        hub.run("Reading the real cartridge save",()->{
            Wire w=hub.require();byte[] rom=Store.read(file);Wire.Cart cart=w.cart();Store.matchRom(rom,cart);
            byte[] save=w.readSave(cart.type,hub.progress("Reading cartridge save"));
            CartridgeSync.match(new Store.Entry(sessionIdentity(id,cart)),w.cart());
            hub.store.begin(id,cart,save);hub.message="Cartridge save loaded · starting mGBA";
        },error->{if(error!=null)Ui.error(this,error);else launch(id);});
    }
    Properties sessionIdentity(String id,Wire.Cart c){Properties p=new Properties();p.setProperty("id",id);p.setProperty("type",""+c.type);p.setProperty("cart",c.identity());p.setProperty("title",c.title());p.setProperty("code",c.code());return p;}
    void launch(String id){if(!isFinishing())startActivity(new Intent(this,PlayActivity.class).putExtra("id",id));}
    void maybePrompt(){
        if(isFinishing()||hub.busy||showingPrompt||showingFirmware||hub.firmwareOffer||hub.bootloaderOffer)return;
        try{List<Store.Entry> entries=hub.store.pending();if(entries.isEmpty())return;Store.Entry e=entries.get(0);String key=e.id+e.save+e.interrupted+hub.ready;if(prompted.equals(key))return;prompted=key;pendingMenu(e);}
        catch(IOException e){Ui.error(this,e);}
    }
    void maybeFirmwarePrompt(){
        if(isFinishing()||hub.busy||showingFirmware||showingPrompt)return;
        if(hub.bootloaderOffer){hub.bootloaderOffer=false;firmware();return;}
        if(!hub.firmwareOffer)return;hub.firmwareOffer=false;showingFirmware=true;
        AlertDialog dialog=new AlertDialog.Builder(this).setTitle("GB-Link firmware differs")
            .setMessage("Installed: "+hub.installedFirmware+"\nBundled: "+FirmwareImage.VERSION+" (official stable RP2040 firmware)\n\nInstall the bundled version over USB? A newer installed version would be downgraded. Unknown versions cannot be compared. No firmware is written without your choice.")
            .setPositiveButton("Install…",(d,w)->firmware()).setNegativeButton("Use installed version",(d,w)->hub.continueWithInstalledFirmware()).create();
        dialog.setOnDismissListener(d->showingFirmware=false);dialog.show();
    }
    void firmware(){
        if(hub.busy)return;List<UsbDevice> boot=hub.bootloaders();
        if(boot.size()>1){new AlertDialog.Builder(this).setTitle("Multiple RP2040 bootloaders").setMessage("Leave only the GB-Link adapter connected, then retry.").setPositiveButton("OK",null).show();return;}
        String detail="Official GB-Link RP2040 firmware "+FirmwareImage.VERSION+" is bundled offline and checked with SHA-256. The affected adapter flash sectors are backed up before erasing and read back after writing. Phone cartridge saves are retained.\n\n";
        if(boot.size()==1){new AlertDialog.Builder(this).setTitle("Install firmware on this GB-Link?")
            .setMessage(detail+"Confirm that the connected RP2040 BOOTSEL device is your GB-Link adapter. Keep USB connected until verification finishes.")
            .setPositiveButton("Install & verify",(d,w)->hub.installFirmware(boot.get(0))).setNegativeButton("Cancel",null).show();
        }else new AlertDialog.Builder(this).setTitle("Prepare GB-Link for firmware update")
            .setMessage(detail+"Exit any game first. Use Enter BOOTSEL to restart a connected GB-Link. If the installed firmware cannot reboot, unplug the adapter, hold its BOOTSEL button and reconnect it to the phone, then tap Install bundled firmware again.")
            .setPositiveButton("Enter BOOTSEL",(d,w)->hub.enterBootloader(e->{if(e!=null)Ui.error(this,e);}))
            .setNegativeButton("Cancel",null).show();
    }
    void pendingMenu(Store.Entry e){
        if(showingPrompt||isFinishing())return;showingPrompt=true;
        AlertDialog dialog=new AlertDialog.Builder(this).setTitle("Write "+e.title+" save back?")
            .setMessage(hub.ready?"Your latest in-game save is on this phone. Write it to the matching cartridge now?":"Reconnect GB-Link and switch on your GBA holding START + SELECT to write the latest save back. You can keep it pending and resume this same session on the phone.")
            .setPositiveButton(hub.ready?"Write to cartridge":"Connect GB-Link",(d,w)->{if(hub.ready)writePending(e.id,null);else connect();})
            .setNegativeButton("Later",null).setNeutralButton("More…",(d,w)->new AlertDialog.Builder(this).setTitle(e.title+" · pending save")
                .setItems(new String[]{"Resume this phone session","Export pending .sav","Write to cartridge"},(x,i)->{
                    if(i==0){try{if(!hub.store.rom(e.id).exists())throw new IOException("Reimport the same ROM to resume this session");launch(e.id);}catch(IOException err){Ui.error(this,err);}}
                    else if(i==1){try{exportBytes=hub.store.data(hub.store.entry(e.id));exportFile=null;export(e.code+"-pending.sav");}catch(IOException err){Ui.error(this,err);}}
                    else writePending(e.id,null);
                }).show()).create();
        dialog.setOnDismissListener(d->showingPrompt=false);dialog.show();
    }
    void writePending(String id,String force){
        if(!hub.ready){connect();return;}
        hub.sync(id,force,error->{
            render();
            if(error instanceof CartridgeSync.Conflict){
                CartridgeSync.Conflict conflict=(CartridgeSync.Conflict)error;
                new AlertDialog.Builder(this).setTitle("Both saves have changed")
                    .setMessage(conflict.getMessage()+"\n\nReplace cartridge save with phone save? The cartridge copy will be backed up before any write.")
                    .setPositiveButton("Back up & replace",(d,w)->writePending(id,conflict.currentHash)).setNegativeButton("Keep both",null).show();
            }else if(error!=null)Ui.error(this,error);
        });
    }
    void pick(int code){if(hub.busy)return;Intent i=new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*");startActivityForResult(i,code);}
    void export(String name){startActivityForResult(new Intent(Intent.ACTION_CREATE_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("application/octet-stream").putExtra(Intent.EXTRA_TITLE,name),EXPORT);}
    void backup(){hub.run("Reading cartridge save for backup",()->{
        Wire w=hub.require();Wire.Cart c=w.cart();if(c.type==0)throw new IOException("No save chip detected");byte[] b=w.readSave(c.type,hub.progress("Backing up save"));
        String key=Store.hash(c.header);hub.store.backup(key,"manual",b);exportBytes=b;exportFile=null;hub.message="Cartridge save backed up";
    },e->{if(e!=null)Ui.error(this,e);else export("cartridge-backup.sav");});}
    void restore(byte[] data){
        hub.run("Checking cartridge and save size",()->{
            Wire w=hub.require();Wire.Cart c=w.cart();if(c.type==0||data.length!=Wire.SIZES[c.type])throw new IOException("Save file must be exactly "+Wire.SIZES[c.type]+" bytes for this cartridge");
            byte[] old=w.readSave(c.type,hub.progress("Reading cartridge before restore"));hub.store.backup(Store.hash(c.header),"before-import",old);
            // Imported saves use the matching local ROM's journal where possible.
            File selected=null;for(File f:hub.store.games()){byte[] rom=Store.read(f);if(Arrays.equals(Arrays.copyOf(rom,192),c.header)){selected=f;break;}}
            if(selected==null)throw new IOException("Import this cartridge's matching ROM first so its save has a durable recovery journal.");
            String id=Store.id(selected);hub.store.begin(id,c,old);hub.store.checkpoint(id,data);exportBytes=null;
            hub.main.post(()->new AlertDialog.Builder(this).setTitle("Replace cartridge save?").setMessage(c.title()+" · "+Wire.TYPES[c.type]+"\nThe imported file is staged as a pending save. Your original cartridge save is backed up.")
                .setPositiveButton("Write & verify",(d,which)->writePending(id,null)).setNegativeButton("Keep pending",null).show());
        },e->{if(e!=null)Ui.error(this,e);render();});
    }
    void dump(){
        cancelDump=false;hub.run("Detecting cartridge ROM",()->{
            Wire w=hub.require();Wire.Cart c=w.cart();
            File partial=new File(getCacheDir(),"cartridge-dump.part");
            try(OutputStream out=new FileOutputStream(partial)){
                w.dump(c,out,(d,t)->{if(cancelDump)throw new IOException("ROM dump cancelled; incomplete dump was not added to the library");hub.progress("Dumping "+c.title()).update(d,t);});
            }
            Wire.Cart after=w.cart();if(!after.identity().equals(c.identity()))throw new IOException("Cartridge changed during dump");
            byte[] bytes=Store.read(partial);if(!Arrays.equals(Arrays.copyOf(bytes,192),c.header))throw new IOException("Dump header mismatch");
            exportFile=hub.store.importRom(bytes);exportBytes=null;
            hub.message="ROM dumped · SHA-256 "+Store.hash(bytes).substring(0,16)+(c.guessed?" · size was estimated by helper":"");
        },e->{render();if(e!=null)Ui.error(this,e);else new AlertDialog.Builder(this).setTitle("ROM added to phone library").setMessage(hub.message+"\nEach transferred block passed its checksum. This does not certify a known-good dump.")
            .setPositiveButton("Export .gba",(d,w)->export("cartridge-dump.gba")).setNegativeButton("Done",null).show();});
        render();
    }
    void backups(){
        File[] fs=hub.store.backups.listFiles();if(fs==null||fs.length==0){toast("No backups yet");return;}
        Arrays.sort(fs,Comparator.comparingLong(File::lastModified).reversed());String[] names=new String[fs.length];for(int i=0;i<fs.length;i++)names[i]=fs[i].getName();
        new AlertDialog.Builder(this).setTitle("Export backup").setItems(names,(d,i)->{exportBytes=null;exportFile=fs[i];export(fs[i].getName());}).show();
    }
    @Override protected void onActivityResult(int request,int result,Intent data){
        super.onActivityResult(request,result,data);if(result!=RESULT_OK||data==null||data.getData()==null)return;Uri uri=data.getData();
        if(request==EXPORT){
            hub.run("Exporting file",()->{try(OutputStream out=getContentResolver().openOutputStream(uri,"wt")){if(out==null)throw new IOException("Cannot open export destination");if(exportBytes!=null)out.write(exportBytes);else if(exportFile!=null)try(InputStream in=new FileInputStream(exportFile)){byte[] b=new byte[8192];int n;while((n=in.read(b))!=-1)out.write(b,0,n);}else throw new IOException("No export selected");}hub.message="File exported";},e->{if(e!=null)Ui.error(this,e);else toast("Exported");});
        }else if(request==IMPORT_ROM){hub.run("Importing phone ROM",()->{try(InputStream in=getContentResolver().openInputStream(uri)){if(in==null)throw new IOException("Cannot read ROM");hub.store.importRom(Hub.bytes(in,32*1024*1024));}hub.message="ROM added · connect its cartridge to play";},e->{if(e!=null)Ui.error(this,e);render();});}
        else if(request==IMPORT_SAVE){try(InputStream in=getContentResolver().openInputStream(uri)){if(in==null)throw new IOException("Cannot read save");restore(Hub.bytes(in,131072));}catch(IOException e){Ui.error(this,e);}}
    }
    void toast(String s){Toast.makeText(this,s,Toast.LENGTH_LONG).show();}
}
