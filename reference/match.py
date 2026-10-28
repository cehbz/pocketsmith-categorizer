import json,collections,sys
from datetime import datetime,timedelta
W='/tmp/claude-0/-home-user/5cae1da0-105f-5daf-87b6-0ae8af662e82/scratchpad/grab/'
ps=json.load(open(W+'ps.json')); g=json.load(open(W+'union.json'))
# ET date of each grab booking (BKK UTC+7 -> ET UTC-4 in summer, -5 after Nov 1)
for a in g:
    bkk=datetime.fromisoformat(a['dt']); a['et']=(bkk-timedelta(hours=11)).date()
# estimate rate: for big orders (>=300 THB) find PS charge same ET date or +-1 with ratio 30..36, pick closest ratio cluster
ratios=[]
for a in g:
    if a['amount']<300: continue
    for x in ps:
        d=datetime.fromisoformat(x['date']).date()
        if abs((d-a['et']).days)<=2:
            r=a['amount']/-x['amt']
            if 30<r<37: ratios.append((a['dt'][:7],round(r,2)))
bym=collections.defaultdict(list)
for m,r in ratios: bym[m].append(r)
for m in sorted(bym):
    c=collections.Counter(round(r,1) for r in bym[m]).most_common(3); print(m,c)
RATE={'2026-04':32.5,'2026-05':32.5,'2026-06':32.5,'2026-07':33.0,'2026-08':32.9,'2026-09':33.0,'2026-10':33.6}
for x in ps:
    x['d']=datetime.fromisoformat(x['date']).date(); x['thb']=-x['amt']*RATE[x['date'][:7]]
used=set(); pairs=[]
for tol in (0.012,0.025,0.04):
  for a in sorted([a for a in g if a['amount']>0],key=lambda a:-a['amount']):
    if a.get('m'): continue
    best=None
    for x in ps:
        if x['id'] in used: continue
        dd=(x['d']-a['et']).days
        if dd<0 or dd>3: continue
        err=abs(x['thb']-a['amount'])/a['amount']
        if err<=tol and (best is None or (dd,err)<best[0]): best=((dd,err),x)
    if best:
        used.add(best[1]['id']); a['m']=best[1]['id']; pairs.append((a,best[1]))
um_g=[a for a in g if not a.get('m')]; um_p=[x for x in ps if x['id'] not in used]
print('grab',len(g),'matched',len(pairs),'unmatched grab',len(um_g),'unmatched ps',len(um_p))
print('unmatched grab by fleet',collections.Counter(a['fleet'] for a in um_g))
print('matched by fleet',collections.Counter(a['fleet'] for a,_ in pairs))
print('unmatched PS THB amounts',collections.Counter(int(round(x['thb']/5)*5) for x in um_p).most_common(15))
print('unmatched ps total USD',round(sum(x['amt'] for x in um_p),2),'n',len(um_p))
print('unmatched ps by month',collections.Counter(x['date'][:7] for x in um_p))
json.dump(dict(pairs=[(a['booking'],x['id']) for a,x in pairs],um_g=[a['booking'] for a in um_g],um_p=[x['id'] for x in um_p]),open(W+'match.json','w'))
for a in um_g[:15]: print(' UG',a['dt'],a['fleet'],a['amount'])
