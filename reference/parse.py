import re, sys, json
from datetime import datetime
DATE = re.compile(r'^\s{0,3}(\d{2} [A-Z][a-z]{2} \d{4}),')
TIME = re.compile(r'(\d{2}:\d{2}[AP]M)')
AMT = re.compile(r'(?<![A-Za-z:])\s(?:THB)\s+([\d,]+\.\d{2})\s*$')
FLEETS = ['Standard (JustGrab)','JustGrab (SVB)','GrabExpress','GrabFood','GrabMart','Standard','Premium','Taxi','RodDaeng','GrabBike','Bike']
def parse(path, src):
    lines = open(path, encoding='utf-8').read().split('\n')
    blocks=[]; cur=None
    for ln in lines:
        m=DATE.match(ln)
        if m:
            if cur: blocks.append(cur)
            cur=[ln]
        elif cur is not None:
            if 'Date/Time' in ln or 'Member Statement' in ln: 
                blocks.append(cur); cur=None; continue
            cur.append(ln)
    if cur: blocks.append(cur)
    out=[]
    for b in blocks:
        text='\n'.join(b)
        d=DATE.match(b[0]).group(1)
        t=TIME.search(text)
        amt=None; amtline=None
        for i,ln in enumerate(b):
            m=re.search(r'THB\s+([\d,]+\.\d{2})\s*$', ln)
            if m: amt=float(m.group(1).replace(',','')); amtline=ln; break
        # booking code: join "A-" with continuation
        flat=re.sub(r'\s+',' ',text)
        bc=None
        m=re.search(r'\b(A-[A-Z0-9]{8,})', text)
        if m: bc=m.group(1)
        else:
            m2=re.search(r'\b([A-Z0-9]{12,}AV)\b', text)
            if m2: bc='A-'+m2.group(1)
        fleet=None
        if amtline:
            pre=amtline[:amtline.rfind('THB')].rstrip()
            for f in FLEETS:
                if pre.endswith(f): fleet=f; break
        if fleet is None:
            if 'GrabExpress' in text: fleet='GrabExpress'
            else:
                for f in FLEETS:
                    if re.search(r'\s%s\s'%re.escape(f), text): fleet=f; break
        dt=datetime.strptime(d+' '+(t.group(1) if t else '12:00AM'),'%d %b %Y %I:%M%p')
        out.append(dict(src=src, dt=dt.isoformat(), booking=bc, fleet=fleet, amount=amt, raw=flat[:200]))
    return out
rows=parse(sys.argv[1],'f0')+parse(sys.argv[2],'f1')
json.dump(rows, open(sys.argv[3],'w'), ensure_ascii=False, indent=1)
print(len(rows))
